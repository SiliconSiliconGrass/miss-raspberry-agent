package napcat_test

import (
	"testing"

	"github.com/tidwall/gjson"

	"miss-raspberry-agent/internal/napcat"
)

// TestParseHistoryResponseReadsDataMessages 复现线上问题：NapCat 的
// get_friend_msg_history / get_group_msg_history 把消息列表放在 data.messages，
// 不能直接把 data 当数组解析（否则会得到一条全 0 的空消息）。
func TestParseHistoryResponseReadsDataMessages(t *testing.T) {
	body := `{"status":"ok","retcode":0,"data":{"messages":[
		{"message_id":101,"message_seq":1001,"time":1700000001,"user_id":10001,"sender":{"nickname":"张三"},"message":[{"type":"text","data":{"text":"你好"}},{"type":"image","data":{"file":"a.png"}}]},
		{"message_id":102,"message_seq":0,"time":1700000002,"user_id":10002,"sender":{"nickname":"李四"},"message":"在吗"},
		{"message_id":103,"message_seq":1003,"time":1700000003,"user_id":10003,"sender":{"nickname":"王五"},"message":[{"type":"image","data":{"file":"b.png"}}]}
	]}}`

	messages := napcat.ParseHistoryResponse(gjson.Get(body, "data"))
	if len(messages) != 3 {
		t.Fatalf("expected 3 messages, got %d: %+v", len(messages), messages)
	}

	first := messages[0]
	if first.MessageID != 101 || first.MessageSeq != 1001 || first.Time != 1700000001 ||
		first.UserID != 10001 || first.NickName != "张三" || first.Content != "你好" {
		t.Errorf("unexpected first message: %+v", first)
	}
	// message_seq 缺失时回退到 message_id。
	if second := messages[1]; second.MessageSeq != 102 || second.Content != "在吗" {
		t.Errorf("unexpected second message: %+v", second)
	}
	// 无文本消息保留在结果里（由上层工具决定是否过滤）。
	if third := messages[2]; third.Content != "" {
		t.Errorf("expected empty content for image-only message, got %+v", third)
	}
}

// TestParseHistoryResponseAcceptsRawArray 兼容 data 本身就是消息数组的返回。
func TestParseHistoryResponseAcceptsRawArray(t *testing.T) {
	body := `[{"message_id":1,"time":1700000001,"user_id":7,"message":"hi"}]`
	messages := napcat.ParseHistoryResponse(gjson.Get(body, "@this"))
	if len(messages) != 1 || messages[0].Content != "hi" {
		t.Fatalf("unexpected messages: %+v", messages)
	}
}
