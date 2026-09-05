package main_agent_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"miss-raspberry-agent/internal/agent/main_agent"
	"miss-raspberry-agent/internal/tools/todo_list"
)

// countingModel records the number of Generate calls; when err is non-nil, it returns the error
// every time.
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

// TestRunProcessesTodoQueue verifies that the Run loop processes an item pushed into the
// agent's queue and completes it, using a group-style item.
func TestRunProcessesTodoQueue(t *testing.T) {
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

	queue.Add(todo_list.Item{
		Content:    "hi",
		Source:     "群聊(群号=1,发送者QQ=2)",
		TargetType: "group",
		TargetID:   1,
		UserID:     2,
	})
	waitModelCalls(t, fake, 2)
	waitTodoEmpty(t, queue)

	last := fake.lastInput()
	if !strings.Contains(joinedContents(last), "hi") {
		t.Errorf("agent input should contain the queued message, got:\n%s", joinedContents(last))
	}
}

// TestRunDoesNotProcessEmptyQueue verifies that an idle Run loop never invokes the model.
func TestRunDoesNotProcessEmptyQueue(t *testing.T) {
	fake := &countingModel{}

	ctx, cancel := context.WithCancel(context.Background())
	agent, err := main_agent.NewMainAgent(ctx, fake, stubSender{}, stubHistory{})
	if err != nil {
		t.Fatalf("NewMainAgent: %v", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		agent.Run(ctx)
	}()

	time.Sleep(500 * time.Millisecond)
	if fake.callCount() != 0 {
		t.Fatalf("empty queue should not invoke the model, model called %d times", fake.callCount())
	}
	cancel()
	<-done
}

// TestDrainRemovesFirstTodoAfterMaxErrorRetries verifies that when the model keeps failing, the
// drain removes the first todo item after maxErrorRetries failures and ends the round.
func TestDrainRemovesFirstTodoAfterMaxErrorRetries(t *testing.T) {
	fake := &countingModel{err: errors.New("model boom")}

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

	queue.Add(todo_list.Item{
		Content:    "任务",
		Source:     "私聊(用户QQ=111)",
		TargetType: "private",
		TargetID:   111,
		UserID:     111,
	})
	waitCount(t, fake, 5)
	waitTodoEmpty(t, queue)

	if got := fake.callCount(); got != 5 {
		t.Fatalf("expected exactly %d model calls after error retry cap, got %d", 5, got)
	}
}

// TestStalledQueueIsNotRetried verifies that a queue the model cannot clear is not drained again
// on every poll tick, and that a new item re-triggers processing.
func TestStalledQueueIsNotRetried(t *testing.T) {
	fake := &countingModel{} // succeeds but never completes the todo item

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

	queue.Add(todo_list.Item{
		Content:    "first",
		Source:     "私聊(用户QQ=111)",
		TargetType: "private",
		TargetID:   111,
		UserID:     111,
	})
	// The first drain runs maxRewakeAttempts+1 successful rounds and then stalls.
	waitCount(t, fake, 6)

	// While the queue is stalled, waiting longer must not trigger extra model calls.
	time.Sleep(700 * time.Millisecond)
	if got := fake.callCount(); got != 6 {
		t.Fatalf("stalled queue should not be retried, model calls = %d, want 6", got)
	}

	// A new item wakes processing again.
	queue.Add(todo_list.Item{
		Content:    "second",
		Source:     "私聊(用户QQ=111)",
		TargetType: "private",
		TargetID:   111,
		UserID:     111,
	})
	waitCount(t, fake, 12)
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
