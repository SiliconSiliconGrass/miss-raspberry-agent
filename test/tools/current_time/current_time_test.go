package current_time_test

import (
	"context"
	"testing"
	"time"

	"miss-raspberry-agent/internal/tools/current_time"
)

func TestCurrentTime(t *testing.T) {
	out, err := current_time.CurrentTime(context.Background(), &current_time.CurrentTimeInput{})
	if err != nil {
		t.Fatalf("CurrentTime: %v", err)
	}
	if out.Timezone != "Asia/Shanghai (UTC+8)" {
		t.Fatalf("unexpected timezone: %q", out.Timezone)
	}
	if _, err := time.Parse("2006-01-02 15:04:05", out.Time); err != nil {
		t.Fatalf("time not formatted as Beijing datetime: %q", out.Time)
	}
	if _, err := time.Parse("2006-01-02", out.Date); err != nil {
		t.Fatalf("date not formatted: %q", out.Date)
	}
	if out.Unix <= 0 {
		t.Fatalf("unexpected unix: %d", out.Unix)
	}
	if out.Weekday == "" {
		t.Fatal("weekday should not be empty")
	}
}

func TestCurrentTimeToolRegistered(t *testing.T) {
	tool := current_time.NewCurrentTimeTool()
	info, err := tool.Info(context.Background())
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if info.Name != "current_time" {
		t.Fatalf("tool name = %q, want current_time", info.Name)
	}
	if info.Desc == "" {
		t.Fatal("tool desc should not be empty")
	}
}
