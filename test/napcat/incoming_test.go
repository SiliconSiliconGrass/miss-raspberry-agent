package napcat_test

import (
	"encoding/json"
	"testing"

	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"

	"miss-raspberry-agent/internal/napcat"
	"miss-raspberry-agent/internal/tools/todo_list"
)

func TestPrivateMessageQueued(t *testing.T) {
	todo := todo_list.NewStore()
	client := napcat.NewClient(nil)
	client.SetTodoList(todo)

	if !client.HandleIncomingMessage(napcat.Message{
		MessageType: "private",
		UserID:      111,
		Content:     "hi",
	}) {
		t.Fatal("private message should be queued")
	}

	items := todo.List()
	if len(items) != 1 {
		t.Fatalf("expected 1 todo item, got %d", len(items))
	}
	item := items[0]
	if item.Content != "hi" {
		t.Errorf("content = %q, want %q", item.Content, "hi")
	}
	if item.Source != "私聊(用户QQ=111)" {
		t.Errorf("source = %q, want %q", item.Source, "私聊(用户QQ=111)")
	}
	if item.TargetType != "private" || item.TargetID != 111 || item.UserID != 111 {
		t.Errorf("unexpected reply fields: %+v", item)
	}
}

func TestGroupMessageWithoutMentionNotQueued(t *testing.T) {
	todo := todo_list.NewStore()
	client := napcat.NewClient(nil)
	client.SetTodoList(todo)

	if client.HandleIncomingMessage(napcat.Message{
		MessageType: "group",
		GroupID:     1,
		UserID:      2,
		Content:     "hi",
	}) {
		t.Fatal("group message without a bot mention should not be queued")
	}
	if !todo.IsEmpty() {
		t.Fatalf("expected empty todo list, got %d items", todo.Len())
	}
}

func TestGroupMessageWithMentionQueued(t *testing.T) {
	todo := todo_list.NewStore()
	client := napcat.NewClient(nil)
	client.SetTodoList(todo)

	// The event is built in the real post-processing shape used by ZeroBot: the at segment has
	// been stripped from Event.Message, and the original at segment is kept in NativeMessage.
	if !client.HandleIncomingMessage(napcat.Message{
		MessageType: "group",
		GroupID:     1,
		UserID:      2,
		Content:     "hi",
		RawEvent: &zero.Event{
			SelfID:        100,
			NativeMessage: json.RawMessage(`[{"type":"at","data":{"qq":"100"}},{"type":"text","data":{"text":"hi"}}]`),
			Message:       message.Message{{Type: "text", Data: map[string]string{"text": "hi"}}},
		},
	}) {
		t.Fatal("group message mentioning the bot should be queued")
	}

	items := todo.List()
	if len(items) != 1 {
		t.Fatalf("expected 1 todo item, got %d", len(items))
	}
	item := items[0]
	if item.Source != "群聊(群号=1,发送者QQ=2)" {
		t.Errorf("source = %q, want %q", item.Source, "群聊(群号=1,发送者QQ=2)")
	}
	if item.TargetType != "group" || item.TargetID != 1 || item.UserID != 2 {
		t.Errorf("unexpected reply fields: %+v", item)
	}
}

func TestIncomingWithoutTodoListConfiguredIsDropped(t *testing.T) {
	client := napcat.NewClient(nil)
	if client.HandleIncomingMessage(napcat.Message{
		MessageType: "private",
		UserID:      111,
		Content:     "hi",
	}) {
		t.Fatal("message should be dropped when no todo queue is configured")
	}
}
