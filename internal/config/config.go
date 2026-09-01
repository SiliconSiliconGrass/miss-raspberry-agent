// Package config loads service configuration from environment variables.
package config

import (
	"errors"
	"os"
)

const (
	envModelAPIKey  = "MODEL_API_KEY"
	envModelBaseURL = "MODEL_BASE_URL"
	envModelName    = "MODEL_NAME"

	envNapcatWebSocketURL = "NAPCAT_WS_URL"
	envNapcatAccessToken  = "NAPCAT_ACCESS_TOKEN"

	defaultBaseURL            = "https://api.openai.com/v1"
	defaultModelStr           = "gpt-4o-mini"
	defaultNapcatWebSocketURL = "ws://127.0.0.1:3001"
)

// Config is the top-level application configuration.
type Config struct {
	Model  ModelConfig
	Napcat NapcatConfig
}

// ModelConfig describes which LLM endpoint to call.
type ModelConfig struct {
	APIKey  string
	BaseURL string
	Name    string
}

// NapcatConfig describes the NapCat WebSocket connection.
type NapcatConfig struct {
	WebSocketURL string
	AccessToken  string
}

// Load reads configuration from the process environment.
// Startup fails if a required variable is missing.
func Load() (Config, error) {
	cfg := ModelConfig{
		APIKey: os.Getenv(envModelAPIKey),
	}
	if cfg.APIKey == "" {
		return Config{}, errors.New("environment variable MODEL_API_KEY is required")
	}

	cfg.BaseURL = envOrDefault(envModelBaseURL, defaultBaseURL)
	cfg.Name = envOrDefault(envModelName, defaultModelStr)

	return Config{
		Model: cfg,
		Napcat: NapcatConfig{
			WebSocketURL: envOrDefault(envNapcatWebSocketURL, defaultNapcatWebSocketURL),
			AccessToken:  os.Getenv(envNapcatAccessToken),
		},
	}, nil
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
