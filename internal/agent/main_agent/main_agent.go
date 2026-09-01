// Package main_agent 实现绑定到 NapCat 客户端的 QQ 助手 agent：
// 私聊消息时被激活，群聊消息仅当机器人被 @ 时被激活，然后处理待办列表并回复消息。
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
	"miss-raspberry-agent/internal/tools/qq_message"
	"miss-raspberry-agent/internal/tools/todo_list"
)

const (
	// maxRewakeAttempts 是单次激活内待办列表非空时最多连续唤醒 agent 的次数，
	// 防止 agent 一直无法清空列表时无限循环消耗模型调用。
	maxRewakeAttempts = 5
	// maxErrorRetries 是单次激活内 runOnce 连续失败时 catch 重试的上限；
	// 达到上限后删除第一条待办，避免坏任务一直卡住激活流程。
	maxErrorRetries = 5
	// maxHistoryMessages 是保留的对话历史消息数上限（约 10 轮对话），
	// 超出时丢弃最早的记录，避免上下文无限增长。
	maxHistoryMessages = 20
)

// MainAgent 是绑定到 NapCat 客户端的 QQ 助手。
type MainAgent struct {
	client *napcat.NapcatClient
	todo   *todo_list.Store
	runner *adk.Runner

	historyMu sync.Mutex
	history   []*schema.Message
}

// NewMainAgent 构建 main_agent：绑定 napcat client，配置 qq_message 的
// 发送/历史两个工具和 todo_list 工具。
func NewMainAgent(ctx context.Context, client *napcat.NapcatClient, chatModel model.BaseChatModel, todo *todo_list.Store) (*MainAgent, error) {
	tools := []tool.BaseTool{
		qq_message.NewQQMessageSender(client),
		qq_message.NewQQMessageGetter(client),
		todo_list.NewTodoListTool(todo),
	}

	// 修复 OpenAI 兼容供应商（如 DeepSeek）对 tool 消息序列的严格校验：
	// 为缺失 content 的 assistant(tool_calls)/tool 消息补上 content 字段。
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
	return &MainAgent{client: client, todo: todo, runner: runner}, nil
}

// Run 监听 NapCat 客户端的消息；私聊消息总是激活 agent，
// 群聊消息仅当机器人被 @ 时才激活。
// 阻塞直到 ctx 取消或客户端停止。
func (a *MainAgent) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			log.Println("[main_agent] context canceled，停止监听")
			return
		case msg, ok := <-a.client.Incoming:
			if !ok {
				log.Println("[main_agent] napcat 客户端已停止")
				return
			}
			if !shouldActivate(msg) {
				log.Printf("[main_agent] 群聊消息未@机器人，不激活（发送者=%d 内容=%s）", msg.UserID, msg.Content)
				continue
			}
			a.activate(ctx, msg)
		}
	}
}

// activate 把收到的消息写入待办列表，然后执行 agent；
// 每轮回复结束后检查待办列表，非空则再次唤醒，直到清空或达到最大唤醒次数。
func (a *MainAgent) activate(ctx context.Context, msg napcat.Message) {
	log.Printf("[main_agent] 收到%s消息：%s", describeSource(msg), msg.Content)

	a.todo.Add(todo_list.Item{
		Content:    msg.Content,
		Source:     describeSource(msg),
		TargetType: msg.MessageType,
		TargetID:   replyTargetID(msg),
		UserID:     msg.UserID,
	})

	errorRetries := 0
	for rewakes := 0; ; {
		if err := a.runOnce(ctx, BuildActivationPrompt(a.todo.List())); err != nil {
			errorRetries++
			log.Printf("[main_agent] 本轮 agent 执行失败（连续第 %d 次，上限 %d 次）: %v", errorRetries, maxErrorRetries, err)
			if errorRetries >= maxErrorRetries {
				if items := a.todo.List(); len(items) > 0 {
					a.todo.Complete(items[0].ID)
					log.Printf("[main_agent] 连续失败 %d 次，已删除第一条待办 %s，本轮结束", maxErrorRetries, items[0].ID)
				}
				return
			}
			log.Println("[main_agent] 已捕获错误，重新执行 agent")
			continue
		}
		errorRetries = 0
		if a.todo.IsEmpty() {
			log.Println("[main_agent] 待办列表已清空，本轮结束")
			return
		}
		rewakes++
		if rewakes > maxRewakeAttempts {
			log.Printf("[main_agent] 连续唤醒 %d 次后待办列表仍不为空（剩余 %d 项），停止本轮", maxRewakeAttempts, a.todo.Len())
			return
		}
		log.Printf("[main_agent] 待办列表仍有 %d 项，再次唤醒 agent", a.todo.Len())
	}
}

// runOnce 运行一轮 agent 并消费全部事件。本轮的用户消息和模型输出/工具结果
// 会在成功后写入历史，使后续激活能携带上下文记忆。
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
			log.Printf("[main_agent] agent 事件错误: %v", event.Err)
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
				log.Printf("[main_agent] 工具调用: %s 参数: %s", tc.Function.Name, tc.Function.Arguments)
			}
			if mv.Message.Content != "" {
				log.Printf("[main_agent] 模型回复: %s", mv.Message.Content)
			}
		case schema.Tool:
			runMsgs = append(runMsgs, mv.Message)
			log.Printf("[main_agent] 工具结果: %s => %s", mv.ToolName, mv.Message.Content)
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

// shouldActivate 判断消息是否触发 agent：私聊消息总是激活；
// 群聊消息仅当机器人自己被 @ 时才激活。
func shouldActivate(msg napcat.Message) bool {
	if msg.MessageType != "group" {
		return true
	}
	return isBotMentioned(msg)
}

// isBotMentioned 检查群聊消息中是否有 @ 机器人本人的 at 段。
//
// 注意：ZeroBot 在预处理消息事件时，检测到 @ 的是机器人本人就会把该 at 段
// 从 Event.Message 中移除（除非配置 KeepAtMeMessage）。因此这里必须从
// Event.NativeMessage（原始 message 字段，尚未被 ZeroBot 处理）解析消息段，
// 而不能用已经处理过的 Event.Message，否则永远检测不到 @。
func isBotMentioned(msg napcat.Message) bool {
	if msg.RawEvent == nil {
		return false
	}
	selfID := strconv.FormatInt(msg.RawEvent.SelfID, 10)

	segs := message.ParseMessage(msg.RawEvent.NativeMessage)
	if len(segs) == 0 {
		// 兜底：调用方直接构造 Event 且未填 NativeMessage 时，
		// 退回检查已处理过的消息段。
		segs = msg.RawEvent.Message
	}
	for _, seg := range segs {
		if seg.Type == "at" && seg.Data["qq"] == selfID {
			return true
		}
	}
	return false
}

// sanitizeHistory 清理历史中不完整的工具调用片段，避免严格供应商（如 DeepSeek）
// 校验消息序列时报 400：
//   - 丢弃没有前置 assistant(tool_calls) 匹配的孤儿 tool 消息。
//
// 正常情况下历史只在整轮成功后才提交，此处是防御性清理。
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
			// 孤儿 tool 消息：丢弃
		default:
			out = append(out, m)
		}
	}
	return out
}

// withUserMessage 返回历史消息加一条新用户消息的副本（不修改原历史）。
func withUserMessage(history []*schema.Message, userMsg *schema.Message) []*schema.Message {
	messages := make([]*schema.Message, 0, len(history)+1)
	messages = append(messages, history...)
	return append(messages, userMsg)
}

// trimHistory 只保留最近 max 条消息。
func trimHistory(messages []*schema.Message, max int) []*schema.Message {
	if max <= 0 || len(messages) <= max {
		return messages
	}
	return messages[len(messages)-max:]
}

// describeSource 生成可读的消息来源描述，供待办列表展示。
func describeSource(msg napcat.Message) string {
	switch msg.MessageType {
	case "group":
		return fmt.Sprintf("群聊(群号=%d,发送者QQ=%d)", msg.GroupID, msg.UserID)
	default:
		return fmt.Sprintf("私聊(用户QQ=%d)", msg.UserID)
	}
}

// replyTargetID 返回回复消息时应使用的 target_id：
// 群聊回复群号，私聊回复用户QQ号。
func replyTargetID(msg napcat.Message) int64 {
	if msg.MessageType == "group" {
		return msg.GroupID
	}
	return msg.UserID
}
