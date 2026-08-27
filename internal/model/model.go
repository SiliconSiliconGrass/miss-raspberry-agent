// Package model constructs the LLM chat model used by agents.
package model

import (
	"context"

	einoopenai "github.com/cloudwego/eino-ext/components/model/openai"

	"miss-raspberry-agent/internal/config"
)

// New builds an OpenAI-compatible chat model from the given configuration.
// It enforces a JSON-object response format so agent output stays parseable.
func New(ctx context.Context, cfg config.ModelConfig) (*einoopenai.ChatModel, error) {
	return einoopenai.NewChatModel(ctx, &einoopenai.ChatModelConfig{
		APIKey:  cfg.APIKey,
		BaseURL: cfg.BaseURL,
		Model:   cfg.Name,
		ResponseFormat: &einoopenai.ChatCompletionResponseFormat{
			Type: einoopenai.ChatCompletionResponseFormatTypeJSONObject,
		},
	})
}
