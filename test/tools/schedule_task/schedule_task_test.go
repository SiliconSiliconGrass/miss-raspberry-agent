package schedule_task_test

import (
	"context"
	"testing"
	"time"

	"miss-raspberry-agent/internal/scheduler"
	"miss-raspberry-agent/internal/tools/schedule_task"
)

func TestScheduleTaskCreateListCancel(t *testing.T) {
	store := scheduler.NewStoreWithLocation(time.FixedZone("Asia/Shanghai", 8*3600))
	current := func() scheduler.Source {
		return scheduler.Source{Description: "私聊(用户QQ=123)", TargetType: "private", TargetID: 123, UserID: 123}
	}

	out, err := schedule_task.RunScheduleTask(context.Background(), store, current, &schedule_task.ScheduleTaskInput{
		Action:  "create",
		Content: "提醒我喝水",
		Type:    "weekly",
		Weekday: "Sun",
		Time:    "08:00",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if out.Task == nil || out.Task.ID == "" {
		t.Fatalf("expected created task, got %+v", out)
	}
	createdID := out.Task.ID
	if out.Task.TargetType != "private" || out.Task.TargetID != 123 {
		t.Fatalf("source not captured: %+v", out.Task)
	}
	if out.Task.NextRunText() == "-" {
		t.Fatalf("expected next run text, got %q", out.Task.NextRunText())
	}
	if out.Task.Schedule != "every Sun at 08:00" {
		t.Fatalf("unexpected schedule description: %q", out.Task.Schedule)
	}

	out, err = schedule_task.RunScheduleTask(context.Background(), store, current, &schedule_task.ScheduleTaskInput{Action: "list"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(out.Tasks) != 1 || out.Tasks[0].ID != createdID {
		t.Fatalf("unexpected list result: %+v", out.Tasks)
	}

	out, err = schedule_task.RunScheduleTask(context.Background(), store, current, &schedule_task.ScheduleTaskInput{
		Action: "cancel",
		TaskID: createdID,
	})
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if len(store.List()) != 0 {
		t.Fatal("store should be empty after cancel")
	}

	// Canceling again returns a "does not exist" notice instead of an error.
	out, err = schedule_task.RunScheduleTask(context.Background(), store, current, &schedule_task.ScheduleTaskInput{
		Action: "cancel",
		TaskID: createdID,
	})
	if err != nil {
		t.Fatalf("cancel again should not error: %v", err)
	}
	if out.Task != nil {
		t.Fatalf("expected no task in output, got %+v", out.Task)
	}
}

func TestScheduleTaskOverridesSource(t *testing.T) {
	store := scheduler.NewStoreWithLocation(time.FixedZone("Asia/Shanghai", 8*3600))
	current := func() scheduler.Source {
		return scheduler.Source{Description: "私聊(用户QQ=1)", TargetType: "private", TargetID: 1}
	}
	out, err := schedule_task.RunScheduleTask(context.Background(), store, current, &schedule_task.ScheduleTaskInput{
		Action:     "create",
		Content:    "发到群里",
		Type:       "daily",
		Time:       "09:00",
		TargetType: "group",
		TargetID:   999,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if out.Task.TargetType != "group" || out.Task.TargetID != 999 {
		t.Fatalf("override not applied: %+v", out.Task)
	}
}

func TestScheduleTaskValidation(t *testing.T) {
	store := scheduler.NewStoreWithLocation(time.FixedZone("Asia/Shanghai", 8*3600))
	current := func() scheduler.Source { return scheduler.Source{} }
	cases := []struct {
		name string
		in   *schedule_task.ScheduleTaskInput
	}{
		{"unknown action", &schedule_task.ScheduleTaskInput{Action: "delete"}},
		{"create without content", &schedule_task.ScheduleTaskInput{Action: "create", Type: "daily", Time: "09:00"}},
		{"create without type", &schedule_task.ScheduleTaskInput{Action: "create", Content: "x"}},
		{"unknown type", &schedule_task.ScheduleTaskInput{Action: "create", Content: "x", Type: "hourly"}},
		{"once without datetime", &schedule_task.ScheduleTaskInput{Action: "create", Content: "x", Type: "once"}},
		{"bad datetime", &schedule_task.ScheduleTaskInput{Action: "create", Content: "x", Type: "once", Datetime: "随便"}},
		{"relative without delay", &schedule_task.ScheduleTaskInput{Action: "create", Content: "x", Type: "relative"}},
		{"relative negative", &schedule_task.ScheduleTaskInput{Action: "create", Content: "x", Type: "relative", Minutes: -5}},
		{"daily without time", &schedule_task.ScheduleTaskInput{Action: "create", Content: "x", Type: "daily"}},
		{"bad time", &schedule_task.ScheduleTaskInput{Action: "create", Content: "x", Type: "daily", Time: "25:00"}},
		{"weekly without weekday", &schedule_task.ScheduleTaskInput{Action: "create", Content: "x", Type: "weekly", Time: "08:00"}},
		{"weekly invalid weekday", &schedule_task.ScheduleTaskInput{Action: "create", Content: "x", Type: "weekly", Weekday: "周", Time: "08:00"}},
		{"monthly invalid day", &schedule_task.ScheduleTaskInput{Action: "create", Content: "x", Type: "monthly", DayOfMonth: 32, Time: "08:00"}},
		{"yearly invalid date", &schedule_task.ScheduleTaskInput{Action: "create", Content: "x", Type: "yearly", Date: "13-01", Time: "08:00"}},
		{"target type without id", &schedule_task.ScheduleTaskInput{Action: "create", Content: "x", Type: "daily", Time: "09:00", TargetType: "group"}},
		{"cancel without id", &schedule_task.ScheduleTaskInput{Action: "cancel"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := schedule_task.RunScheduleTask(context.Background(), store, current, tc.in); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestScheduleTaskToolRegistered(t *testing.T) {
	store := scheduler.NewStoreWithLocation(time.FixedZone("Asia/Shanghai", 8*3600))
	tool := schedule_task.NewScheduleTaskTool(store, func() scheduler.Source { return scheduler.Source{} })
	info, err := tool.Info(context.Background())
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if info.Name != "schedule_task" {
		t.Fatalf("tool name = %q, want schedule_task", info.Name)
	}
}
