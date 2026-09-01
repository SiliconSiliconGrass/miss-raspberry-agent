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

// 消息结构体，用于内部通信
type Message struct {
	UserID      int64
	GroupID     int64
	MessageType string
	NickName    string
	Content     string
	RawEvent    *zero.Event
}

// HistoryMessage 一条历史消息记录。Content 仅包含文本内容，
// 非文本消息（如图片、语音）Content 为空字符串。
type HistoryMessage struct {
	MessageID  int64
	MessageSeq int64
	Time       int64
	UserID     int64
	NickName   string
	Content    string
}

// NapcatClient 结构体
type NapcatClient struct {
	// 配置
	config *NapcatClientConfig

	// 消息通道
	// Incoming: 收到的消息（NapCat -> Agent）
	// Outgoing: 要发送的消息（Agent -> NapCat）
	Incoming chan Message
	Outgoing chan Message

	// 控制
	mu      sync.RWMutex
	running bool
	done    chan struct{}
}

// 默认配置
func DefaultConfig() *NapcatClientConfig {
	return &NapcatClientConfig{
		WebSocketURL:  "ws://127.0.0.1:3001",
		AccessToken:   "",
		NickName:      []string{"bot"},
		CommandPrefix: "/",
		SuperUsers:    []int64{},
	}
}

// 构造函数
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

// 启动客户端（非阻塞）
func (c *NapcatClient) Start() error {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return nil
	}
	c.running = true
	c.mu.Unlock()

	// 注册消息处理器
	zero.OnMessage(func(ctx *zero.Ctx) bool {
		// 可以在这里加过滤逻辑
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

		// 非阻塞发送到 Incoming channel
		select {
		case c.Incoming <- msg:
			log.Printf("[Napcat] 收到消息: %s", msg.Content)
		default:
			log.Println("[Napcat] Incoming channel 已满，丢弃消息")
		}
	})

	// 启动 Outgoing 消息处理 goroutine
	go c.processOutgoing()

	// 启动 ZeroBot（在 goroutine 中运行，避免阻塞）
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

// 停止客户端
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

// 发送私聊消息（Agent 调用）
func (c *NapcatClient) SendMessage(userID int64, content string) bool {
	msg := Message{
		UserID:  userID,
		Content: content,
	}

	if !c.enqueue(msg) {
		return false
	}
	log.Printf("[Napcat] 消息已加入发送队列: %s", content)
	return true
}

// 发送群消息（Agent 调用）
func (c *NapcatClient) SendGroupMessage(groupID int64, content string) bool {
	msg := Message{
		GroupID: groupID,
		Content: content,
	}

	if !c.enqueue(msg) {
		return false
	}
	log.Printf("[Napcat] 群消息已加入发送队列: %s", content)
	return true
}

// enqueue 把消息放入 Outgoing 队列；队列已满时返回 false
func (c *NapcatClient) enqueue(msg Message) bool {
	select {
	case c.Outgoing <- msg:
		return true
	default:
		log.Println("[Napcat] Outgoing channel 已满，发送失败")
		return false
	}
}

// GetMessageHistory 获取私聊（private）或群聊（group）的历史消息。
//
// beforeSeq 为 0 时返回最新的 count 条；大于 0 时返回 message_seq 小于
// beforeSeq 的 count 条，用于向前翻页。返回的消息按接口顺序排列。
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

	messages := make([]HistoryMessage, 0, len(rsp.Data.Array()))
	for _, item := range rsp.Data.Array() {
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
	return messages, nil
}

// extractText 从一条 OneBot 消息中提取纯文本内容。
// message 可能是字符串，也可能是消息段数组；只保留 type=text 的段。
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

// 处理 Outgoing 消息的 goroutine
func (c *NapcatClient) processOutgoing() {
	for {
		select {
		case <-c.done:
			return
		case msg := <-c.Outgoing:
			// 通过 ZeroBot 发送消息
			zero.RangeBot(func(_ int64, ctx *zero.Ctx) bool {
				if msg.GroupID != 0 {
					ctx.SendGroupMessage(msg.GroupID, msg.Content)
				} else {
					ctx.SendPrivateMessage(msg.UserID, msg.Content)
				}
				return false
			})
			log.Printf("[Napcat] 消息已发送: %s", msg.Content)
		}
	}
}

// 获取消息（阻塞式，Agent 使用）
func (c *NapcatClient) GetMessage() (Message, bool) {
	msg, ok := <-c.Incoming
	return msg, ok
}
