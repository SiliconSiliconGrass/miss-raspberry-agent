package main_agent_test

import (
	"context"

	"miss-raspberry-agent/internal/napcat"
)

// stubSender implements qq_message.Sender without touching a real NapCat client.
type stubSender struct{}

func (stubSender) SendMessage(_ int64, _ string) bool { return true }

func (stubSender) SendGroupMessage(_ int64, _ string) bool { return true }

// stubHistory implements qq_message.HistoryProvider and always returns an empty history.
type stubHistory struct{}

func (stubHistory) GetMessageHistory(context.Context, string, int64, int64, int) ([]napcat.HistoryMessage, error) {
	return nil, nil
}
