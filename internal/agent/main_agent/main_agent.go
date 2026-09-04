// Package main_agent implements a QQ assistant agent bound to a NapCat client:
// it activates on private messages and on group messages only when the bot is @-mentioned,
// then processes the todo list and replies to messages.
package main_agent

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"sync"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"github.com/wdvxdr1123/ZeroBot/message"

	"miss-raspberry-agent/internal/napcat"
	"miss-raspberry-agent/internal/scheduler"
	"miss-raspberry-agent/internal/tools/current_time"
	"miss-raspberry-agent/internal/tools/qq_message"
	"miss-raspberry-agent/internal/tools/schedule_task"
	"miss-raspberry-agent/internal/tools/todo_list"
)

const (
	// maxRewakeAttempts is the maximum number of consecutive agent rewakes within a single
	// activation while the todo list is non-empty, preventing an infinite loop that burns model
	// calls when the agent never manages to clear the list.
	maxRewakeAttempts = 5
	// maxErrorRetries caps the catch-up retries when runOnce keeps failing within a single
	// activation; once the cap is reached the first todo item is removed, so a bad task cannot
	// keep blocking the activation flow.
	maxErrorRetries = 5
	// maxHistoryMessages is the upper bound on the number of conversation history messages kept
	// (roughly 10 turns); older entries are dropped beyond it to keep the context from growing unbounded.
	maxHistoryMessages = 20
)

// MainAgent is a QQ assistant bound to a NapCat client.
type MainAgent struct {
	client *napcat.NapcatClient
	todo   *todo_list.Store
	sched  *scheduler.Scheduler
	runner *adk.Runner

	historyMu sync.Mutex
	history   []*schema.Message

	sourceMu sync.Mutex
	source   scheduler.Source
}

// NewMainAgent builds the main_agent: it binds the napcat client and wires up the qq_message
// send/history tools, todo_list, current_time, and schedule_task tools.
func NewMainAgent(ctx context.Context, client *napcat.NapcatClient, chatModel model.BaseChatModel, todo *todo_list.Store) (*MainAgent, error) {
	sched := scheduler.NewScheduler(scheduler.NewStore())
	a := &MainAgent{client: client, todo: todo, sched: sched}

	tools := []tool.BaseTool{
		current_time.NewCurrentTimeTool(),
		schedule_task.NewScheduleTaskTool(sched.Store(), a.currentSource),
		qq_message.NewQQMessageSender(client),
		qq_message.NewQQMessageGetter(client),
		todo_list.NewTodoListTool(todo),
	}

	// Work around strict OpenAI-compatible providers (e.g. DeepSeek) validating tool message
	// sequences: add a content field to assistant(tool_calls)/tool messages that lack one.
	chatModel = &patchOpenAIPayloadModel{inner: chatModel}

	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        "main_agent",
		Description: "QQ 助手：回复私聊/群聊消息并维护待办列表",
		Instruction: BaseSystemPrompt,
		Model:       chatModel,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{
				Tools: tools,
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("construct main agent: %w", err)
	}

	runner := adk.NewRunner(ctx, adk.RunnerConfig{Agent: agent})
	a.runner = runner
	return a, nil
}

// Run listens for messages from the NapCat client; private messages always activate the agent,
// while group messages do so only when the bot is @-mentioned. It also listens for scheduled-task
// trigger events. It blocks until ctx is canceled or the client stops.
func (a *MainAgent) Run(ctx context.Context) {
	schedCtx, cancelSched := context.WithCancel(ctx)
	defer cancelSched()
	go a.sched.Run(schedCtx)

	for {
		select {
		case <-ctx.Done():
			log.Println("[main_agent] context canceled, stop listening")
			return
		case msg, ok := <-a.client.Incoming:
			if !ok {
				log.Println("[main_agent] napcat client has stopped")
				return
			}
			if !shouldActivate(msg) {
				log.Printf("[main_agent] group message does not @ the bot, not activating (sender=%d content=%s)", msg.UserID, msg.Content)
				continue
			}
			a.activate(ctx, msg)
		case task, ok := <-a.sched.Fires():
			if !ok {
				continue
			}
			a.activateScheduled(ctx, task)
		}
	}
}

// activate writes the received message into the todo list, then runs the agent; after each round
// of replies it checks the todo list and rewakes the agent while non-empty, until the list is
// cleared or the maximum rewake count is reached.
func (a *MainAgent) activate(ctx context.Context, msg napcat.Message) {
	log.Printf("[main_agent] received %s message: %s", describeSource(msg), msg.Content)

	a.setCurrentSource(scheduler.Source{
		Description: describeSource(msg),
		TargetType:  msg.MessageType,
		TargetID:    replyTargetID(msg),
		UserID:      msg.UserID,
	})
	a.todo.Add(todo_list.Item{
		Content:    msg.Content,
		Source:     describeSource(msg),
		TargetType: msg.MessageType,
		TargetID:   replyTargetID(msg),
		UserID:     msg.UserID,
	})

	a.drainTodoList(ctx)
}

// activateScheduled adds a scheduled task to the todo list as an item when it fires, then rewakes
// the agent to handle it; the reply target reuses the message source captured when the task was created.
func (a *MainAgent) activateScheduled(ctx context.Context, task scheduler.Task) {
	log.Printf("[main_agent] scheduled task fired: %s (%s, rule %s)", task.ID, task.Content, task.Schedule)

	a.setCurrentSource(scheduler.Source{
		Description: task.Source,
		TargetType:  task.TargetType,
		TargetID:    task.TargetID,
		UserID:      task.UserID,
	})
	a.todo.Add(todo_list.Item{
		Content:    fmt.Sprintf("[定时任务 %s] %s", task.Schedule, task.Content),
		Source:     task.Source,
		TargetType: task.TargetType,
		TargetID:   task.TargetID,
		UserID:     task.UserID,
	})

	a.drainTodoList(ctx)
}

// drainTodoList runs one round of the agent and, if the todo list still has items, rewakes it
// until the list is empty or the rewake/error caps are reached. It returns once the current
// activation's work is fully handled (or abandoned after the caps).
func (a *MainAgent) drainTodoList(ctx context.Context) {
	errorRetries := 0
	for rewakes := 0; ; {
		if err := a.runOnce(ctx, BuildActivationPrompt(a.todo.List())); err != nil {
			errorRetries++
			log.Printf("[main_agent] agent execution failed this round (%d consecutive failures, limit %d): %v", errorRetries, maxErrorRetries, err)
			if errorRetries >= maxErrorRetries {
				if items := a.todo.List(); len(items) > 0 {
					a.todo.Complete(items[0].ID)
					log.Printf("[main_agent] failed %d consecutive times, removed the first todo %s, ending this round", maxErrorRetries, items[0].ID)
				}
				return
			}
			log.Println("[main_agent] error caught, re-running the agent")
			continue
		}
		errorRetries = 0
		if a.todo.IsEmpty() {
			log.Println("[main_agent] todo list cleared, ending this round")
			return
		}
		rewakes++
		if rewakes > maxRewakeAttempts {
			log.Printf("[main_agent] todo list still not empty after %d consecutive rewakes (%d items remaining), stopping this round", maxRewakeAttempts, a.todo.Len())
			return
		}
		log.Printf("[main_agent] todo list still has %d items, rewaking the agent", a.todo.Len())
	}
}

// currentSource returns the source of the message currently being handled, used by tools such as
// schedule_task as the default reply target.
func (a *MainAgent) currentSource() scheduler.Source {
	a.sourceMu.Lock()
	defer a.sourceMu.Unlock()
	return a.source
}

func (a *MainAgent) setCurrentSource(src scheduler.Source) {
	a.sourceMu.Lock()
	defer a.sourceMu.Unlock()
	a.source = src
}

// runOnce runs one round of the agent and consumes all events. The user message and the model
// outputs/tool results of this round are committed to history on success, so later activations
// carry conversational context.
func (a *MainAgent) runOnce(ctx context.Context, prompt string) error {
	userMsg := schema.UserMessage(prompt)

	a.historyMu.Lock()
	runHistory := withUserMessage(sanitizeHistory(a.history), userMsg)
	a.historyMu.Unlock()

	iter := a.runner.Run(ctx, runHistory)

	var (
		runMsgs []*schema.Message
		lastErr error
	)
	for {
		event, ok := iter.Next()
		if !ok {
			break
		}
		if event.Err != nil {
			log.Printf("[main_agent] agent event error: %v", event.Err)
			lastErr = event.Err
		}
		if event.Output == nil || event.Output.MessageOutput == nil {
			continue
		}
		mv := event.Output.MessageOutput
		if mv.Message == nil {
			continue
		}
		switch mv.Role {
		case schema.Assistant:
			runMsgs = append(runMsgs, mv.Message)
			for _, tc := range mv.Message.ToolCalls {
				log.Printf("[main_agent] tool call: %s args: %s", tc.Function.Name, tc.Function.Arguments)
			}
			if mv.Message.Content != "" {
				log.Printf("[main_agent] model reply: %s", mv.Message.Content)
			}
		case schema.Tool:
			runMsgs = append(runMsgs, mv.Message)
			log.Printf("[main_agent] tool result: %s => %s", mv.ToolName, mv.Message.Content)
		}
	}
	if lastErr != nil {
		return lastErr
	}

	a.historyMu.Lock()
	a.history = trimHistory(sanitizeHistory(append(withUserMessage(a.history, userMsg), runMsgs...)), maxHistoryMessages)
	a.historyMu.Unlock()
	return nil
}

// shouldActivate decides whether a message triggers the agent: private messages always activate it;
// group messages do so only when the bot itself is @-mentioned.
func shouldActivate(msg napcat.Message) bool {
	if msg.MessageType != "group" {
		return true
	}
	return isBotMentioned(msg)
}

// isBotMentioned checks whether a group message contains an at segment mentioning the bot itself.
//
// Note: when ZeroBot pre-processes a message event, it removes any at segment that mentions the
// bot itself from Event.Message (unless KeepAtMeMessage is configured). Therefore we must parse
// segments from Event.NativeMessage (the raw message field, not yet processed by ZeroBot) rather
// than from the already-processed Event.Message, otherwise the @ mention would never be detected.
func isBotMentioned(msg napcat.Message) bool {
	if msg.RawEvent == nil {
		return false
	}
	selfID := strconv.FormatInt(msg.RawEvent.SelfID, 10)

	segs := message.ParseMessage(msg.RawEvent.NativeMessage)
	if len(segs) == 0 {
		// Fallback: when the caller constructs the Event directly and left NativeMessage empty,
		// fall back to inspecting the already-processed message segments.
		segs = msg.RawEvent.Message
	}
	for _, seg := range segs {
		if seg.Type == "at" && seg.Data["qq"] == selfID {
			return true
		}
	}
	return false
}

// sanitizeHistory cleans up incomplete tool-call fragments in the history, avoiding a 400 from
// strict providers (e.g. DeepSeek) when they validate the message sequence:
//   - drop orphan tool messages that have no preceding assistant(tool_calls) to match them.
//
// Normally history is only committed after a whole round succeeds; this is defensive cleanup.
func sanitizeHistory(messages []*schema.Message) []*schema.Message {
	out := make([]*schema.Message, 0, len(messages))
	pendingIDs := map[string]bool{}
	for _, m := range messages {
		switch m.Role {
		case schema.Assistant:
			if len(m.ToolCalls) > 0 {
				pendingIDs = map[string]bool{}
				for _, tc := range m.ToolCalls {
					pendingIDs[tc.ID] = true
				}
				out = append(out, m)
				continue
			}
			pendingIDs = map[string]bool{}
			out = append(out, m)
		case schema.Tool:
			if len(pendingIDs) > 0 && pendingIDs[m.ToolCallID] {
				delete(pendingIDs, m.ToolCallID)
				out = append(out, m)
				continue
			}
			// Orphan tool message: dropped
		default:
			out = append(out, m)
		}
	}
	return out
}

// withUserMessage returns a copy of the history plus one new user message (the original history
// is not modified).
func withUserMessage(history []*schema.Message, userMsg *schema.Message) []*schema.Message {
	messages := make([]*schema.Message, 0, len(history)+1)
	messages = append(messages, history...)
	return append(messages, userMsg)
}

// trimHistory keeps only the most recent max messages.
func trimHistory(messages []*schema.Message, max int) []*schema.Message {
	if max <= 0 || len(messages) <= max {
		return messages
	}
	return messages[len(messages)-max:]
}

// describeSource produces a human-readable description of the message source, for display in the
// todo list.
func describeSource(msg napcat.Message) string {
	switch msg.MessageType {
	case "group":
		return fmt.Sprintf("群聊(群号=%d,发送者QQ=%d)", msg.GroupID, msg.UserID)
	default:
		return fmt.Sprintf("私聊(用户QQ=%d)", msg.UserID)
	}
}

// replyTargetID returns the target_id to use when replying to a message: the group ID for group
// chats and the user's QQ number for private chats.
func replyTargetID(msg napcat.Message) int64 {
	if msg.MessageType == "group" {
		return msg.GroupID
	}
	return msg.UserID
}
