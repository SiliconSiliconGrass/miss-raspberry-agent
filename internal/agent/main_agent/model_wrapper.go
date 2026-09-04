package main_agent

import (
	"context"
	"encoding/json"

	einoopenai "github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// patchOpenAIPayloadModel wraps the chat model and injects a request-body modifier before each call.
// Background: go-openai serializes with omitempty, so empty content fields are dropped entirely.
// Strict providers such as DeepSeek v4 therefore reject assistant messages carrying tool_calls
// (erroring with "Messages with role 'tool' must be a response to a preceding message with 'tool_calls'")
// as well as tool messages without content. Here we uniformly add the content field back.
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

// patchMissingContent adds a content field (an empty string when absent) to assistant(tool_calls)
// and tool messages in the request body, keeping the payload compatible with the sequence
// validation of strict OpenAI-compatible providers.
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
