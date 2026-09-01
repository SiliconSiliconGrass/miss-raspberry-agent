package qq_message

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"time"

	"miss-raspberry-agent/internal/napcat"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
)

const (
	defaultHistoryCount = 20
	maxHistoryCount     = 100
	historyPageSize     = 50
	maxHistoryFetch     = 500
)

type QQMessageGetterInput struct {
	TargetType string `json:"target_type" jsonschema:"enum=private,enum=group,description=目标类型：private=私聊，group=群聊"`
	TargetId   int64  `json:"target_id" jsonschema:"required,description=私聊目标用户QQ号或群聊QQ群号"`
	Count      int    `json:"count" jsonschema:"default=20,description=要获取的最近消息条数，仅在未指定时间范围时生效，默认 20，最大 100"`
	StartTime  int64  `json:"start_time" jsonschema:"description=时间范围起点（Unix 秒），可选"`
	EndTime    int64  `json:"end_time" jsonschema:"description=时间范围终点（Unix 秒），可选"`
}

// QQHistoryMessage 返回给 agent 的一条历史消息。
type QQHistoryMessage struct {
	MessageID int64  `json:"message_id"`
	Time      int64  `json:"time"`
	UserID    int64  `json:"user_id"`
	NickName  string `json:"nickname"`
	Content   string `json:"content"`
}

type QQMessageGetterOutput struct {
	Messages []QQHistoryMessage `json:"messages"`
}

// HistoryProvider 是历史消息来源，由 *napcat.NapcatClient 实现；
// 拆成接口是为了在测试中替换消息来源。
type HistoryProvider interface {
	GetMessageHistory(ctx context.Context, targetType string, targetID, beforeSeq int64, count int) ([]napcat.HistoryMessage, error)
}

var _ HistoryProvider = (*napcat.NapcatClient)(nil)

// GetMessageHistory 按条数或时间范围获取指定目标（私聊/群聊）的文本消息历史，
// 结果按时间从新到旧排列。只保留包含文本内容的消息。
func GetMessageHistory(ctx context.Context, provider HistoryProvider, in *QQMessageGetterInput) (*QQMessageGetterOutput, error) {
	if err := validateGetterInput(in); err != nil {
		return nil, err
	}

	var (
		raw []napcat.HistoryMessage
		err error
	)
	if in.StartTime > 0 || in.EndTime > 0 {
		raw, err = fetchInTimeRange(ctx, provider, in)
	} else {
		raw, err = provider.GetMessageHistory(ctx, in.TargetType, in.TargetId, 0, normalizeCount(in.Count))
	}
	if err != nil {
		return nil, fmt.Errorf("qq_message_getter: %w", err)
	}
	return buildHistoryOutput(raw), nil
}

func validateGetterInput(in *QQMessageGetterInput) error {
	switch in.TargetType {
	case "private", "group":
	default:
		return fmt.Errorf("qq_message_getter: unknown target_type %q (expected private or group)", in.TargetType)
	}
	if in.TargetId <= 0 {
		return errors.New("qq_message_getter: target_id is required")
	}
	if in.StartTime > 0 && in.EndTime > 0 && in.StartTime > in.EndTime {
		return errors.New("qq_message_getter: start_time must not be later than end_time")
	}
	return nil
}

func normalizeCount(count int) int {
	if count <= 0 {
		return defaultHistoryCount
	}
	if count > maxHistoryCount {
		return maxHistoryCount
	}
	return count
}

// fetchInTimeRange 从最新的消息开始向前翻页，取到覆盖 [start_time, end_time]
// 的时间段为止（最多 maxHistoryFetch 条），再筛选出时间范围内的消息。
func fetchInTimeRange(ctx context.Context, provider HistoryProvider, in *QQMessageGetterInput) ([]napcat.HistoryMessage, error) {
	startTime := in.StartTime
	endTime := in.EndTime
	if endTime == 0 {
		endTime = time.Now().Unix()
	}

	var (
		all     []napcat.HistoryMessage
		seen    = map[int64]bool{}
		nextSeq int64
		prevSeq int64
		total   int
	)
	for {
		page, err := provider.GetMessageHistory(ctx, in.TargetType, in.TargetId, nextSeq, historyPageSize)
		if err != nil {
			return nil, err
		}
		if len(page) == 0 {
			break
		}
		sortHistoryNewestFirst(page)

		for _, m := range page {
			if m.MessageID != 0 {
				if seen[m.MessageID] {
					continue
				}
				seen[m.MessageID] = true
			}
			all = append(all, m)
		}
		total += len(page)

		oldest := page[len(page)-1]
		if oldest.Time < startTime || total >= maxHistoryFetch {
			break
		}
		nextSeq = oldest.MessageSeq
		if nextSeq <= 0 || nextSeq == prevSeq {
			break
		}
		prevSeq = nextSeq
	}

	selected := make([]napcat.HistoryMessage, 0, len(all))
	for _, m := range all {
		if m.Time >= startTime && m.Time <= endTime {
			selected = append(selected, m)
		}
	}
	return selected, nil
}

// buildHistoryOutput 把历史消息整理成输出格式：按时间从新到旧排列，
// 并且只保留有文本内容的消息。
func buildHistoryOutput(raw []napcat.HistoryMessage) *QQMessageGetterOutput {
	sortHistoryNewestFirst(raw)
	out := &QQMessageGetterOutput{}
	for _, m := range raw {
		if m.Content == "" {
			continue
		}
		out.Messages = append(out.Messages, QQHistoryMessage{
			MessageID: m.MessageID,
			Time:      m.Time,
			UserID:    m.UserID,
			NickName:  m.NickName,
			Content:   m.Content,
		})
	}
	return out
}

func sortHistoryNewestFirst(messages []napcat.HistoryMessage) {
	sort.SliceStable(messages, func(i, j int) bool {
		return messages[i].Time > messages[j].Time
	})
}

// NewQQMessageGetter 构造“获取QQ消息历史”工具。
func NewQQMessageGetter(napcatClient *napcat.NapcatClient) tool.BaseTool {
	fn := func(ctx context.Context, in *QQMessageGetterInput) (*QQMessageGetterOutput, error) {
		return GetMessageHistory(ctx, napcatClient, in)
	}
	tool, err := utils.InferTool(
		"qq_message_getter", // tool name，LLM 靠这个调用
		"获取指定QQ用户或群聊的文本消息历史：可按条数（count）或时间范围（start_time/end_time）获取，返回按时间倒序的消息列表",
		fn,
	)
	if err != nil {
		log.Fatal(err)
	}
	return tool
}
