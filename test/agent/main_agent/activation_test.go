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

// countingModel 记录 Generate 调用次数；err 非 nil 时每次都返回错误。
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

	// 群聊消息未@机器人：不应激活。
	client.Incoming <- napcat.Message{MessageType: "group", GroupID: 1, UserID: 2, Content: "hi"}
	time.Sleep(300 * time.Millisecond)
	if fake.callCount() != 0 {
		t.Fatalf("group message without mention should not activate, model called %d times", fake.callCount())
	}

	// 群聊消息@了机器人：应激活。这里按 ZeroBot 处理后的真实形态构造事件：
	// Event.Message 中的 at 段已被 ZeroBot 剥离，原始 at 段在 NativeMessage 里。
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

	// 私聊消息激活后模型一直失败：连续 5 次后应删除第一条待办并结束本轮。
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
