package napcat

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"

	"github.com/tidwall/gjson"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/driver"
)

// Message is the struct used for internal communication.
type Message struct {
	UserID      int64
	GroupID     int64
	MessageType string
	NickName    string
	Content     string
	RawEvent    *zero.Event
}

// HistoryMessage is a record of one historical message. Content only contains
// the text payload; non-text messages (e.g. images, voice) leave Content empty.
type HistoryMessage struct {
	MessageID  int64
	MessageSeq int64
	Time       int64
	UserID     int64
	NickName   string
	Content    string
}

// NapcatClient struct.
type NapcatClient struct {
	// Configuration.
	config *NapcatClientConfig

	// Message channels.
	// Incoming: received messages (NapCat -> Agent)
	// Outgoing: messages to send (Agent -> NapCat)
	Incoming chan Message
	Outgoing chan Message

	// Control.
	mu      sync.RWMutex
	running bool
	done    chan struct{}
}

// DefaultConfig.
func DefaultConfig() *NapcatClientConfig {
	return &NapcatClientConfig{
		WebSocketURL:  "ws://127.0.0.1:3001",
		AccessToken:   "",
		NickName:      []string{"bot"},
		CommandPrefix: "/",
		SuperUsers:    []int64{},
	}
}

// Constructor.
func NewClient(cfg *NapcatClientConfig) *NapcatClient {
	if cfg == nil {
		cfg = DefaultConfig()
	}

	return &NapcatClient{
		config:   cfg,
		Incoming: make(chan Message, 100),
		Outgoing: make(chan Message, 100),
		done:     make(chan struct{}),
	}
}

// Start starts the client (non-blocking).
func (c *NapcatClient) Start() error {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return nil
	}
	c.running = true
	c.mu.Unlock()

	// Register the message handler.
	zero.OnMessage(func(ctx *zero.Ctx) bool {
		// Filtering logic can be added here.
		return true
	}).Handle(func(ctx *zero.Ctx) {
		nickname := ""
		if ctx.Event.Sender != nil {
			nickname = ctx.Event.Sender.NickName
		}
		msg := Message{
			UserID:      ctx.Event.UserID,
			GroupID:     ctx.Event.GroupID,
			MessageType: ctx.Event.MessageType,
			NickName:    nickname,
			Content:     ctx.Event.Message.String(),
			RawEvent:    ctx.Event,
		}

		// Send to the Incoming channel without blocking.
		select {
		case c.Incoming <- msg:
			log.Printf("[Napcat] received message: %s", msg.Content)
		default:
			log.Println("[Napcat] Incoming channel is full, dropping message")
		}
	})

	// Start the goroutine that processes Outgoing messages.
	go c.processOutgoing()

	// Start ZeroBot (run in a goroutine to avoid blocking).
	go func() {
		zero.Run(&zero.Config{
			NickName:      c.config.NickName,
			CommandPrefix: c.config.CommandPrefix,
			SuperUsers:    c.config.SuperUsers,
			Driver: []zero.Driver{
				driver.NewWebSocketClient(c.config.WebSocketURL, c.config.AccessToken),
			},
		})
	}()

	log.Println("[Napcat] Client started")
	return nil
}

// Stop stops the client.
func (c *NapcatClient) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.running {
		return
	}

	c.running = false
	close(c.done)
	close(c.Incoming)
	close(c.Outgoing)

	log.Println("[Napcat] Client stopped")
}

// SendMessage sends a private message (called by the Agent).
func (c *NapcatClient) SendMessage(userID int64, content string) bool {
	msg := Message{
		UserID:  userID,
		Content: content,
	}

	if !c.enqueue(msg) {
		return false
	}
	log.Printf("[Napcat] message queued for sending: %s", content)
	return true
}

// SendGroupMessage sends a group message (called by the Agent).
func (c *NapcatClient) SendGroupMessage(groupID int64, content string) bool {
	msg := Message{
		GroupID: groupID,
		Content: content,
	}

	if !c.enqueue(msg) {
		return false
	}
	log.Printf("[Napcat] group message queued for sending: %s", content)
	return true
}

// enqueue puts a message into the Outgoing queue; returns false if the queue is full.
func (c *NapcatClient) enqueue(msg Message) bool {
	select {
	case c.Outgoing <- msg:
		return true
	default:
		log.Println("[Napcat] Outgoing channel is full, send failed")
		return false
	}
}

// GetMessageHistory fetches historical messages for a private or group chat.
//
// When beforeSeq is 0 it returns the latest count messages; when greater than 0
// it returns count messages whose message_seq is less than beforeSeq, for paging
// backwards. The returned messages are ordered as returned by the API.
func (c *NapcatClient) GetMessageHistory(ctx context.Context, targetType string, targetID, beforeSeq int64, count int) ([]HistoryMessage, error) {
	action := "get_group_msg_history"
	params := zero.Params{
		"group_id": targetID,
		"count":    count,
	}
	if targetType == "private" {
		action = "get_friend_msg_history"
		params = zero.Params{
			"user_id": strconv.FormatInt(targetID, 10),
			"count":   count,
		}
	}
	if beforeSeq > 0 {
		if targetType == "private" {
			params["message_seq"] = strconv.FormatInt(beforeSeq, 10)
		} else {
			params["message_seq"] = beforeSeq
			params["message_id"] = beforeSeq
		}
	}

	var rsp zero.APIResponse
	called := false
	zero.RangeBot(func(_ int64, zb *zero.Ctx) bool {
		called = true
		rsp = zb.CallActionWithContext(ctx, action, params)
		return false
	})
	if !called {
		return nil, errors.New("napcat: no bot connected")
	}
	if rsp.RetCode != 0 {
		return nil, fmt.Errorf("napcat: %s failed (retcode=%d, message=%s, wording=%s)", action, rsp.RetCode, rsp.Message, rsp.Wording)
	}

	return ParseHistoryResponse(rsp.Data), nil
}

// ParseHistoryResponse parses the data part of a get_*_msg_history response
// into historical messages. NapCat returns the data as data.messages; this also
// accepts a direct array of messages.
func ParseHistoryResponse(data gjson.Result) []HistoryMessage {
	// Calling Array() directly on data treats the whole object as one "empty
	// message"; the messages field must be read instead.
	messagesResult := data.Get("messages")
	if !messagesResult.Exists() {
		messagesResult = data
	}

	messages := make([]HistoryMessage, 0, len(messagesResult.Array()))
	for _, item := range messagesResult.Array() {
		seq := item.Get("message_seq").Int()
		if seq == 0 {
			seq = item.Get("message_id").Int()
		}
		messages = append(messages, HistoryMessage{
			MessageID:  item.Get("message_id").Int(),
			MessageSeq: seq,
			Time:       item.Get("time").Int(),
			UserID:     item.Get("user_id").Int(),
			NickName:   item.Get("sender.nickname").String(),
			Content:    extractText(item),
		})
	}
	return messages
}

// extractText extracts plain text from a OneBot message.
// message may be a string or an array of message segments; only segments of
// type=text are kept.
func extractText(item gjson.Result) string {
	message := item.Get("message")
	if message.Type == gjson.String {
		return message.String()
	}
	var sb strings.Builder
	for _, seg := range message.Array() {
		if seg.Get("type").String() != "text" {
			continue
		}
		sb.WriteString(seg.Get("data.text").String())
	}
	return sb.String()
}

// processOutgoing is the goroutine that processes Outgoing messages.
func (c *NapcatClient) processOutgoing() {
	for {
		select {
		case <-c.done:
			return
		case msg := <-c.Outgoing:
			// Send the message via ZeroBot.
			zero.RangeBot(func(_ int64, ctx *zero.Ctx) bool {
				if msg.GroupID != 0 {
					ctx.SendGroupMessage(msg.GroupID, msg.Content)
				} else {
					ctx.SendPrivateMessage(msg.UserID, msg.Content)
				}
				return false
			})
			log.Printf("[Napcat] message sent: %s", msg.Content)
		}
	}
}

// GetMessage gets a message (blocking; used by the Agent).
func (c *NapcatClient) GetMessage() (Message, bool) {
	msg, ok := <-c.Incoming
	return msg, ok
}
