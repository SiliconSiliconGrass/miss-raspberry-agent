package main_agent_test

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"miss-raspberry-agent/internal/agent/main_agent"
	"miss-raspberry-agent/internal/tools/todo_list"
)

var itemIDPattern = regexp.MustCompile(`item-\d+`)

// memoryFakeModel returns a todo_list complete tool call on the first Generate of each round
// and fixed text on the second, letting the agent complete the todo, empty the list, and end
// the round normally.
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

// TestMainAgentKeepsConversationMemoryAcrossQueueActivations verifies that each queue item is
// processed and that later activations carry the conversation history of earlier ones.
func TestMainAgentKeepsConversationMemoryAcrossQueueActivations(t *testing.T) {
	fake := &memoryFakeModel{}

	ctx, cancel := context.WithCancel(context.Background())
	agent, err := main_agent.NewMainAgent(ctx, fake, stubSender{}, stubHistory{})
	if err != nil {
		t.Fatalf("NewMainAgent: %v", err)
	}
	queue := agent.Queue()

	done := make(chan struct{})
	go func() {
		defer close(done)
		agent.Run(ctx)
	}()
	defer func() {
		cancel()
		<-done
	}()

	// First round: a private message; the agent should complete the todo item.
	queue.Add(todo_list.Item{
		Content:    "第一条消息",
		Source:     "私聊(用户QQ=111)",
		TargetType: "private",
		TargetID:   111,
		UserID:     111,
	})
	waitModelCalls(t, fake, 2)
	if !queue.IsEmpty() {
		t.Fatalf("expected todo list to be cleared after first activation, got %d items", queue.Len())
	}

	// Second round: a group message. The model should now see the conversation history from the
	// first round.
	queue.Add(todo_list.Item{
		Content:    "第二条消息",
		Source:     "群聊(群号=333,发送者QQ=222)",
		TargetType: "group",
		TargetID:   333,
		UserID:     222,
	})
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

func joinedContents(messages []*schema.Message) string {
	var sb strings.Builder
	for _, m := range messages {
		sb.WriteString(m.Content)
		sb.WriteString("\n")
	}
	return sb.String()
}
