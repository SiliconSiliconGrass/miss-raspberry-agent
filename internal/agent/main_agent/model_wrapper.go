package main_agent

import (
	"context"
	"encoding/json"

	einoopenai "github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// patchOpenAIPayloadModel 包装 chat model，在每次调用前注入请求体修改器。
// 背景：go-openai 序列化时用 omitempty 把空的 content 字段直接省略，
// DeepSeek v4 等严格供应商会因此拒绝带 tool_calls 的 assistant 消息
// （报 "Messages with role 'tool' must be a response to a preceding message with 'tool_calls'"），
// 也会拒绝没有 content 的 tool 消息。这里统一补上 content 字段。
type patchOpenAIPayloadModel struct {
	inner model.BaseChatModel
}

func (m *patchOpenAIPayloadModel) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	opts = append(opts, einoopenai.WithRequestPayloadModifier(patchMissingContent))
	return m.inner.Generate(ctx, input, opts...)
}

func (m *patchOpenAIPayloadModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	opts = append(opts, einoopenai.WithRequestPayloadModifier(patchMissingContent))
	return m.inner.Stream(ctx, input, opts...)
}

// patchMissingContent 给请求体中的 assistant(tool_calls) 消息和 tool 消息补上
// content 字段（缺失时置为空字符串），保证与严格 OpenAI 兼容供应商的序列校验兼容。
func patchMissingContent(_ context.Context, _ []*schema.Message, rawBody []byte) ([]byte, error) {
	var payload map[string]any
	if err := json.Unmarshal(rawBody, &payload); err != nil {
		return nil, err
	}

	messages, _ := payload["messages"].([]any)
	for _, m := range messages {
		msg, ok := m.(map[string]any)
		if !ok {
			continue
		}
		role, _ := msg["role"].(string)
		switch role {
		case "assistant":
			if _, hasToolCalls := msg["tool_calls"]; !hasToolCalls {
				continue
			}
			if _, hasContent := msg["content"]; !hasContent {
				msg["content"] = ""
			}
		case "tool":
			if _, hasContent := msg["content"]; !hasContent {
				msg["content"] = ""
			}
		}
	}

	return json.Marshal(payload)
}
