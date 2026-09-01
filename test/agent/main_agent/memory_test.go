package main_agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"

	"miss-raspberry-agent/internal/agent/main_agent"
	"miss-raspberry-agent/internal/napcat"
	"miss-raspberry-agent/internal/tools/todo_list"
)

var itemIDPattern = regexp.MustCompile(`item-\d+`)

// memoryFakeModel 每轮第一次 Generate 返回 todo_list complete 工具调用，
// 第二次返回固定文本，从而让 agent 完成待办、清空列表后正常结束本轮。
type memoryFakeModel struct {
	mu       sync.Mutex
	calls    int
	messages [][]*schema.Message
}

func (f *memoryFakeModel) Generate(_ context.Context, input []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	f.mu.Lock()
	f.calls++
	f.messages = append(f.messages, copyMessages(input))
	callNum := f.calls
	f.mu.Unlock()

	if callNum%2 == 1 {
		return schema.AssistantMessage("", []schema.ToolCall{{
			ID:   "call-todo",
			Type: "function",
			Function: schema.FunctionCall{
				Name:      "todo_list",
				Arguments: `{"action":"complete","id":"` + extractItemID(input) + `"}`,
			},
		}}), nil
	}
	return schema.AssistantMessage("已处理", nil), nil
}

func (f *memoryFakeModel) Stream(context.Context, []*schema.Message, ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, errors.New("stream not implemented")
}

func copyMessages(input []*schema.Message) []*schema.Message {
	out := make([]*schema.Message, len(input))
	for i, m := range input {
		cp := *m
		cp.ToolCalls = append([]schema.ToolCall(nil), m.ToolCalls...)
		out[i] = &cp
	}
	return out
}

func extractItemID(input []*schema.Message) string {
	for i := len(input) - 1; i >= 0; i-- {
		if input[i].Role != schema.User {
			continue
		}
		if id := itemIDPattern.FindString(input[i].Content); id != "" {
			return id
		}
	}
	return ""
}

func TestMainAgentKeepsConversationMemoryAcrossActivations(t *testing.T) {
	fake := &memoryFakeModel{}
	client := napcat.NewClient(nil)
	todo := todo_list.NewStore()

	ctx, cancel := context.WithCancel(context.Background())
	agent, err := main_agent.NewMainAgent(ctx, client, fake, todo)
	if err != nil {
		t.Fatalf("NewMainAgent: %v", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		agent.Run(ctx)
	}()
	defer func() {
		cancel()
		<-done
	}()

	// 第一轮：私聊消息，agent 应 complete 掉待办。
	client.Incoming <- napcat.Message{MessageType: "private", UserID: 111, Content: "第一条消息"}
	waitModelCalls(t, fake, 2)
	if !todo.IsEmpty() {
		t.Fatalf("expected todo list to be cleared after first activation, got %d items", todo.Len())
	}

	// 第二轮：群聊消息。此时模型应能看到第一轮的对话历史。
	client.Incoming <- napcat.Message{
		MessageType: "group",
		UserID:      222,
		GroupID:     333,
		Content:     "第二条消息",
		RawEvent: &zero.Event{
			SelfID:        100,
			NativeMessage: json.RawMessage(`[{"type":"at","data":{"qq":"100"}},{"type":"text","data":{"text":"第二条消息"}}]`),
			Message:       message.Message{{Type: "text", Data: map[string]string{"text": "第二条消息"}}},
		},
	}
	waitModelCalls(t, fake, 4)

	lastInput := fake.lastInput()
	joined := joinedContents(lastInput)
	for _, want := range []string{"第一条消息", "第二条消息", "已处理"} {
		if !strings.Contains(joined, want) {
			t.Errorf("second activation input should contain %q (conversation memory), got:\n%s", want, joined)
		}
	}
}

func waitModelCalls(t *testing.T, fake *memoryFakeModel, want int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for fake.callCount() < want {
		if time.Now().After(deadline) {
			t.Fatalf("model calls = %d, want >= %d", fake.callCount(), want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (f *memoryFakeModel) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *memoryFakeModel) lastInput() []*schema.Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.messages) == 0 {
		return nil
	}
	return f.messages[len(f.messages)-1]
}

func joinedContents(messages []*schema.Message) string {
	var sb strings.Builder
	for _, m := range messages {
		sb.WriteString(m.Content)
		sb.WriteString("\n")
	}
	return sb.String()
}
