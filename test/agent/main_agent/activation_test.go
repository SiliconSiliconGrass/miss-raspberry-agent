package main_agent_test

import (
	"context"
	"encoding/json"
	"errors"
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

// countingModel records the number of Generate calls; when err is non-nil, it returns the error every time.
type countingModel struct {
	mu    sync.Mutex
	calls int
	err   error
}

func (m *countingModel) Generate(_ context.Context, _ []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	m.mu.Lock()
	m.calls++
	m.mu.Unlock()
	if m.err != nil {
		return nil, m.err
	}
	return schema.AssistantMessage("好的", nil), nil
}

func (m *countingModel) Stream(context.Context, []*schema.Message, ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, errors.New("stream not implemented")
}

func (m *countingModel) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

func TestGroupMessageRequiresMention(t *testing.T) {
	fake := &countingModel{}
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

	// A group message that does not mention the bot should not activate.
	client.Incoming <- napcat.Message{MessageType: "group", GroupID: 1, UserID: 2, Content: "hi"}
	time.Sleep(300 * time.Millisecond)
	if fake.callCount() != 0 {
		t.Fatalf("group message without mention should not activate, model called %d times", fake.callCount())
	}

	// A group message mentioning the bot should activate. The event is built in the real
	// post-processing shape used by ZeroBot: the at segment has been stripped from
	// Event.Message, and the original at segment is kept in NativeMessage.
	client.Incoming <- napcat.Message{
		MessageType: "group",
		GroupID:     1,
		UserID:      2,
		Content:     "hi",
		RawEvent: &zero.Event{
			SelfID:        100,
			NativeMessage: json.RawMessage(`[{"type":"at","data":{"qq":"100"}},{"type":"text","data":{"text":"hi"}}]`),
			Message:       message.Message{{Type: "text", Data: map[string]string{"text": "hi"}}},
		},
	}
	waitCount(t, fake, 1)
}

func TestActivateDeletesFirstTodoAfterMaxErrorRetries(t *testing.T) {
	fake := &countingModel{err: errors.New("model boom")}
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

	// After the private message activates the agent, the model keeps failing: after 5 consecutive
	// failures it should delete the first todo item and end this round.
	client.Incoming <- napcat.Message{MessageType: "private", UserID: 111, Content: "任务"}
	waitCount(t, fake, 5)
	waitTodoEmpty(t, todo)

	if got := fake.callCount(); got != 5 {
		t.Fatalf("expected exactly %d model calls after error retry cap, got %d", 5, got)
	}
}

func waitCount(t *testing.T, fake *countingModel, want int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for fake.callCount() < want {
		if time.Now().After(deadline) {
			t.Fatalf("model calls = %d, want >= %d", fake.callCount(), want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func waitTodoEmpty(t *testing.T, todo *todo_list.Store) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !todo.IsEmpty() {
		if time.Now().After(deadline) {
			t.Fatalf("todo list still has %d items", todo.Len())
		}
		time.Sleep(10 * time.Millisecond)
	}
}
