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

	defaultBaseURL  = "https://api.openai.com/v1"
	defaultModelStr = "gpt-4o-mini"
)

// Config is the top-level application configuration.
type Config struct {
	Model ModelConfig
}

// ModelConfig describes which LLM endpoint to call.
type ModelConfig struct {
	APIKey  string
	BaseURL string
	Name    string
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

	return Config{Model: cfg}, nil
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
