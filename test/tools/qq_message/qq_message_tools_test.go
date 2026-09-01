package qq_message_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"miss-raspberry-agent/internal/napcat"
	"miss-raspberry-agent/internal/tools/qq_message"
)

// fakeHistoryProvider 模拟 NapCat 的历史消息接口：
// messages 按时间从新到旧排列，beforeSeq 大于 0 时只返回更早的消息。
type fakeHistoryProvider struct {
	messages []napcat.HistoryMessage
	mu       sync.Mutex
	calls    []historyCall
}

type historyCall struct {
	targetType string
	targetID   int64
	beforeSeq  int64
	count      int
}

func (f *fakeHistoryProvider) GetMessageHistory(ctx context.Context, targetType string, targetID, beforeSeq int64, count int) ([]napcat.HistoryMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, historyCall{targetType: targetType, targetID: targetID, beforeSeq: beforeSeq, count: count})

	var result []napcat.HistoryMessage
	for _, m := range f.messages {
		if beforeSeq > 0 && m.MessageSeq >= beforeSeq {
			continue
		}
		result = append(result, m)
		if len(result) >= count {
			break
		}
	}
	return result, nil
}

func TestGetMessageHistoryByCount(t *testing.T) {
	provider := &fakeHistoryProvider{messages: historyMessages(t, 5)}

	out, err := qq_message.GetMessageHistory(context.Background(), provider, &qq_message.QQMessageGetterInput{
		TargetType: "group",
		TargetId:   123,
		Count:      3,
	})
	if err != nil {
		t.Fatalf("GetMessageHistory: %v", err)
	}
	if len(out.Messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(out.Messages))
	}
	for i := 1; i < len(out.Messages); i++ {
		if out.Messages[i-1].Time < out.Messages[i].Time {
			t.Fatalf("messages not sorted newest first: %+v", out.Messages)
		}
	}
	if out.Messages[0].MessageID != 105 {
		t.Errorf("expected newest message 105, got %+v", out.Messages[0])
	}

	provider.mu.Lock()
	defer provider.mu.Unlock()
	if len(provider.calls) != 1 {
		t.Fatalf("expected a single history call, got %+v", provider.calls)
	}
	if provider.calls[0].beforeSeq != 0 || provider.calls[0].count != 3 {
		t.Errorf("unexpected history call: %+v", provider.calls[0])
	}
}

func TestGetMessageHistoryKeepsOnlyTextMessages(t *testing.T) {
	provider := &fakeHistoryProvider{messages: []napcat.HistoryMessage{
		{MessageID: 103, MessageSeq: 103, Time: 300, UserID: 1, Content: "hello"},
		{MessageID: 102, MessageSeq: 102, Time: 200, UserID: 2, Content: ""}, // 图片消息
		{MessageID: 101, MessageSeq: 101, Time: 100, UserID: 1, Content: "world"},
	}}

	out, err := qq_message.GetMessageHistory(context.Background(), provider, &qq_message.QQMessageGetterInput{
		TargetType: "private",
		TargetId:   456,
		Count:      10,
	})
	if err != nil {
		t.Fatalf("GetMessageHistory: %v", err)
	}
	if len(out.Messages) != 2 {
		t.Fatalf("expected 2 text messages, got %+v", out.Messages)
	}
	if out.Messages[0].Content != "hello" || out.Messages[1].Content != "world" {
		t.Errorf("unexpected messages: %+v", out.Messages)
	}
}

func TestGetMessageHistoryByTimeRangePaginates(t *testing.T) {
	// 120 条消息，seq 与 time 都从 1000 递增到 1119（新的在后）。
	msgs := make([]napcat.HistoryMessage, 0, 120)
	for seq := int64(1119); seq >= 1000; seq-- {
		msgs = append(msgs, napcat.HistoryMessage{
			MessageID:  seq,
			MessageSeq: seq,
			Time:       seq,
			UserID:     7,
			Content:    "msg",
		})
	}
	provider := &fakeHistoryProvider{messages: msgs}

	out, err := qq_message.GetMessageHistory(context.Background(), provider, &qq_message.QQMessageGetterInput{
		TargetType: "group",
		TargetId:   123,
		StartTime:  1050,
		EndTime:    1100,
	})
	if err != nil {
		t.Fatalf("GetMessageHistory: %v", err)
	}
	if len(out.Messages) != 51 {
		t.Fatalf("expected 51 messages in [1050, 1100], got %d", len(out.Messages))
	}
	if out.Messages[0].Time != 1100 || out.Messages[len(out.Messages)-1].Time != 1050 {
		t.Errorf("expected newest-first range 1100..1050, got %+v", out.Messages)
	}

	provider.mu.Lock()
	defer provider.mu.Unlock()
	if len(provider.calls) < 2 {
		t.Fatalf("expected paging across multiple calls, got %+v", provider.calls)
	}
	if provider.calls[1].beforeSeq == 0 {
		t.Errorf("expected second call to page backwards, got %+v", provider.calls)
	}
}

func TestGetMessageHistoryValidation(t *testing.T) {
	provider := &fakeHistoryProvider{}

	cases := []struct {
		name string
		in   *qq_message.QQMessageGetterInput
	}{
		{"unknown target_type", &qq_message.QQMessageGetterInput{TargetType: "channel", TargetId: 1}},
		{"missing target_id", &qq_message.QQMessageGetterInput{TargetType: "group", TargetId: 0}},
		{"reversed time range", &qq_message.QQMessageGetterInput{TargetType: "group", TargetId: 1, StartTime: 200, EndTime: 100}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := qq_message.GetMessageHistory(context.Background(), provider, tc.in); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestSendMessageRoutesToPrivateAndGroup(t *testing.T) {
	client := napcat.NewClient(nil)

	if _, err := qq_message.SendMessage(context.Background(), client, &qq_message.QQMessageSenderInput{
		TargetId: 123,
		Content:  "hello",
	}); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	select {
	case msg := <-client.Outgoing:
		if msg.UserID != 123 || msg.GroupID != 0 {
			t.Errorf("expected private message to user 123, got %+v", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("private message was not enqueued")
	}

	if _, err := qq_message.SendMessage(context.Background(), client, &qq_message.QQMessageSenderInput{
		TargetId:   456,
		TargetType: "group",
		Content:    "hello",
	}); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	select {
	case msg := <-client.Outgoing:
		if msg.GroupID != 456 || msg.UserID != 0 {
			t.Errorf("expected group message to group 456, got %+v", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("group message was not enqueued")
	}
}

func TestSendMessageRejectsUnknownTargetType(t *testing.T) {
	client := napcat.NewClient(nil)
	_, err := qq_message.SendMessage(context.Background(), client, &qq_message.QQMessageSenderInput{
		TargetId:   1,
		TargetType: "channel",
		Content:    "x",
	})
	if err == nil {
		t.Fatal("expected an error for unknown target_type")
	}
}

// historyMessages 生成 n 条连续的历史消息，最新的在后（seq/time 从 n+100 递增）。
func historyMessages(t *testing.T, n int) []napcat.HistoryMessage {
	t.Helper()
	msgs := make([]napcat.HistoryMessage, 0, n)
	for i := n; i >= 1; i-- {
		seq := int64(i + 100)
		msgs = append(msgs, napcat.HistoryMessage{
			MessageID:  seq,
			MessageSeq: seq,
			Time:       seq,
			UserID:     7,
			Content:    "msg",
		})
	}
	return msgs
}
