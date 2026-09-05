package napcat

import (
	"fmt"
	"log"
	"strconv"

	"github.com/wdvxdr1123/ZeroBot/message"

	"miss-raspberry-agent/internal/tools/todo_list"
)

// HandleIncomingMessage routes one received QQ message into the todo queue: private messages
// always activate the agent, while group messages do so only when the bot itself is @-mentioned.
// It is the shared seam between the ZeroBot event handler and tests; it reports whether the
// message was queued.
func (c *NapcatClient) HandleIncomingMessage(msg Message) bool {
	c.mu.RLock()
	todo := c.todoList
	c.mu.RUnlock()

	log.Printf("[Napcat] received message: %s", msg.Content)
	if !shouldActivate(msg) {
		log.Printf("[Napcat] group message does not @ the bot, not queued (sender=%d content=%s)", msg.UserID, msg.Content)
		return false
	}
	if todo == nil {
		log.Println("[Napcat] no todo queue configured, dropping message")
		return false
	}
	todo.Add(todo_list.Item{
		Content:    msg.Content,
		Source:     describeSource(msg),
		TargetType: msg.MessageType,
		TargetID:   replyTargetID(msg),
		UserID:     msg.UserID,
	})
	return true
}

// shouldActivate decides whether a message triggers the agent: private messages always do;
// group messages do so only when the bot itself is @-mentioned.
func shouldActivate(msg Message) bool {
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
func isBotMentioned(msg Message) bool {
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

// describeSource produces a human-readable description of the message source, for display in the
// todo queue.
func describeSource(msg Message) string {
	switch msg.MessageType {
	case "group":
		return fmt.Sprintf("群聊(群号=%d,发送者QQ=%d)", msg.GroupID, msg.UserID)
	default:
		return fmt.Sprintf("私聊(用户QQ=%d)", msg.UserID)
	}
}

// replyTargetID returns the target_id to use when replying to a message: the group ID for group
// chats and the user's QQ number for private chats.
func replyTargetID(msg Message) int64 {
	if msg.MessageType == "group" {
		return msg.GroupID
	}
	return msg.UserID
}
