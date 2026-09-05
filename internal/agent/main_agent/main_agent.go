// Package main_agent implements a QQ assistant agent driven by a todo queue:
// producers (the NapCat client and the scheduler) push work items into the queue,
// and the agent loop processes whatever it finds there.
package main_agent

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"

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
	// queuePollInterval is how often the Run loop re-checks the todo queue while idle.
	queuePollInterval = 200 * time.Millisecond
)

// MainAgent is a QQ assistant that consumes work from its own todo queue.
type MainAgent struct {
	todo   *todo_list.Store
	sched  *scheduler.Scheduler
	runner *adk.Runner

	historyMu sync.Mutex
	history   []*schema.Message

	sourceMu sync.Mutex
	source   scheduler.Source
}

// NewMainAgent builds the main_agent: it creates the agent's own todo queue and wires up the
// qq_message send/history tools, todo_list, current_time, and schedule_task tools. The sender
// and history arguments are narrow interfaces (implemented by the NapCat client) so the agent
// is not coupled to a concrete transport.
func NewMainAgent(ctx context.Context, chatModel model.BaseChatModel, sender qq_message.Sender, history qq_message.HistoryProvider) (*MainAgent, error) {
	sched := scheduler.NewScheduler(scheduler.NewStore())
	a := &MainAgent{todo: todo_list.NewStore(), sched: sched}

	tools := []tool.BaseTool{
		current_time.NewCurrentTimeTool(),
		schedule_task.NewScheduleTaskTool(sched.Store(), a.currentSource),
		qq_message.NewQQMessageSender(sender),
		qq_message.NewQQMessageGetter(history),
		todo_list.NewTodoListTool(a.todo),
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

// Queue returns the todo queue that producers push work into; it is also the queue the Run loop
// polls.
func (a *MainAgent) Queue() *todo_list.Store {
	return a.todo
}

// Run polls the agent's todo queue until ctx is canceled: whenever the queue is non-empty it
// drains it, and it keeps the scheduler running so scheduled-task firings are also enqueued.
func (a *MainAgent) Run(ctx context.Context) {
	schedCtx, cancelSched := context.WithCancel(ctx)
	defer cancelSched()
	go a.sched.Run(schedCtx)
	go a.forwardScheduledFires(schedCtx)

	// stalledVersion records the queue version when the previous drain ended with a non-empty
	// list. A new drain only starts after the version moves (a new item arrived), so a list the
	// model cannot clear is not retried on every poll tick.
	var stalledVersion int64
	for {
		if ctx.Err() != nil {
			log.Println("[main_agent] context canceled, stop listening")
			return
		}
		if !a.todo.IsEmpty() && a.todo.Version() != stalledVersion {
			a.setCurrentSourceFromQueue()
			a.drainTodoList(ctx)
			if !a.todo.IsEmpty() {
				stalledVersion = a.todo.Version()
			}
		}
		time.Sleep(queuePollInterval)
	}
}

// forwardScheduledFires converts scheduler firing events into todo items so that, exactly like
// NapCat messages, scheduled tasks enter the agent through the queue.
func (a *MainAgent) forwardScheduledFires(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case task, ok := <-a.sched.Fires():
			if !ok {
				return
			}
			a.enqueueScheduledTask(task)
		}
	}
}

// enqueueScheduledTask pushes a fired scheduled task into the queue; the reply target is the
// message source captured when the task was created.
func (a *MainAgent) enqueueScheduledTask(task scheduler.Task) {
	log.Printf("[main_agent] scheduled task fired: %s (%s, rule %s)", task.ID, task.Content, task.Schedule)
	a.todo.Add(todo_list.Item{
		Content:    fmt.Sprintf("[定时任务 %s] %s", task.Schedule, task.Content),
		Source:     task.Source,
		TargetType: task.TargetType,
		TargetID:   task.TargetID,
		UserID:     task.UserID,
	})
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

// setCurrentSourceFromQueue records the reply source of the newest pending item, mirroring the
// previous behavior where the last activation trigger determined the default reply target.
func (a *MainAgent) setCurrentSourceFromQueue() {
	items := a.todo.List()
	if len(items) == 0 {
		return
	}
	last := items[len(items)-1]
	a.setCurrentSource(scheduler.Source{
		Description: last.Source,
		TargetType:  last.TargetType,
		TargetID:    last.TargetID,
		UserID:      last.UserID,
	})
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
