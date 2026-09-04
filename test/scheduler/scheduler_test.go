package scheduler_test

import (
	"testing"
	"time"

	"miss-raspberry-agent/internal/scheduler"
)

func TestStoreAddListCancel(t *testing.T) {
	store := scheduler.NewStoreWithLocation(bj)

	weekly, err := scheduler.NewWeekly("Sun", "08:00")
	if err != nil {
		t.Fatal(err)
	}
	daily, err := scheduler.NewDaily("09:00")
	if err != nil {
		t.Fatal(err)
	}

	first, err := store.Add("提醒喝水", weekly, scheduler.Source{Description: "私聊(用户QQ=1)", TargetType: "private", TargetID: 1, UserID: 1})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	second, err := store.Add("提醒开会", daily, scheduler.Source{Description: "群聊(群号=2)", TargetType: "group", TargetID: 2})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if first.ID == "" || first.ID == second.ID {
		t.Fatalf("expected unique ids, got %q %q", first.ID, second.ID)
	}
	if !first.Repeat || !second.Repeat {
		t.Fatalf("expected both to repeat: %+v %+v", first, second)
	}
	if first.NextRunAt <= 0 || second.NextRunAt <= 0 {
		t.Fatalf("expected positive next run, got %+v %+v", first, second)
	}
	if first.TargetID != 1 || first.TargetType != "private" {
		t.Fatalf("source not captured: %+v", first)
	}
	if first.NextRunText() == "-" || first.NextRunText() == "" {
		t.Fatalf("expected readable next run text, got %q", first.NextRunText())
	}
	if first.Schedule != "every Sun at 08:00" {
		t.Fatalf("unexpected schedule description: %q", first.Schedule)
	}

	tasks := store.List()
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}
	if tasks[0].NextRunAt > tasks[1].NextRunAt {
		t.Fatalf("list not sorted by next run: %+v", tasks)
	}

	got, ok := store.Cancel(first.ID)
	if !ok || got.ID != first.ID {
		t.Fatalf("cancel first failed: %+v %v", got, ok)
	}
	if len(store.List()) != 1 {
		t.Fatalf("expected 1 task after cancel, got %d", len(store.List()))
	}
	if _, ok := store.Cancel(first.ID); ok {
		t.Fatal("canceling the same id twice should fail")
	}
}

func TestStoreAddRejectsPastOrInvalid(t *testing.T) {
	store := scheduler.NewStoreWithLocation(bj)

	past, err := scheduler.NewOnce(time.Date(2020, 1, 1, 10, 0, 0, 0, bj))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2024, 1, 1, 0, 0, 0, 0, bj)
	if _, err := store.AddAt("x", past, scheduler.Source{}, now); err == nil {
		t.Error("AddAt with past once schedule should fail")
	}
	if _, err := store.Add("x", nil, scheduler.Source{}); err == nil {
		t.Error("Add with nil schedule should fail")
	}
}

func TestSchedulerFiresRecurringTask(t *testing.T) {
	store := scheduler.NewStoreWithLocation(bj)
	sched := scheduler.NewScheduler(store)

	start := time.Date(2024, 1, 1, 0, 0, 0, 0, bj) // Monday
	weekly, err := scheduler.NewWeekly("Sun", "08:00")
	if err != nil {
		t.Fatal(err)
	}
	task, err := store.AddAt("周日提醒", weekly, scheduler.Source{}, start)
	if err != nil {
		t.Fatal(err)
	}
	if task.NextRunAt != time.Date(2024, 1, 7, 8, 0, 0, 0, bj).Unix() {
		t.Fatalf("next run = %d, want 2024-01-07 08:00", task.NextRunAt)
	}

	// Does not fire before the scheduled time.
	sched.Tick(time.Date(2024, 1, 7, 7, 59, 59, 0, bj))
	if len(store.List()) != 1 {
		t.Fatal("task should still exist before trigger")
	}

	// Fires once when the time arrives.
	sched.Tick(time.Date(2024, 1, 7, 8, 0, 0, 0, bj))
	evt := waitFire(t, sched)
	if evt.ID != task.ID {
		t.Fatalf("unexpected fired task: %+v", evt)
	}

	// A recurring task should be kept and advanced to the next occurrence.
	remaining := store.List()
	if len(remaining) != 1 {
		t.Fatalf("recurring task should remain, got %d", len(remaining))
	}
	wantNext := time.Date(2024, 1, 14, 8, 0, 0, 0, bj).Unix()
	if remaining[0].NextRunAt != wantNext {
		t.Fatalf("next run = %d, want %d", remaining[0].NextRunAt, wantNext)
	}
}

func TestSchedulerRemovesOneShotTask(t *testing.T) {
	store := scheduler.NewStoreWithLocation(bj)
	sched := scheduler.NewScheduler(store)

	start := time.Date(2024, 1, 7, 8, 0, 0, 0, bj)
	relative, err := scheduler.NewRelative(10 * time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	task, err := store.AddAt("一次提醒", relative, scheduler.Source{}, start)
	if err != nil {
		t.Fatal(err)
	}
	fireAt := start.Add(10 * time.Minute)
	if task.NextRunAt != fireAt.Unix() {
		t.Fatalf("next run = %d, want %d", task.NextRunAt, fireAt.Unix())
	}
	sched.Tick(fireAt.Add(-time.Second))
	if len(store.List()) != 1 {
		t.Fatal("task should not fire before due time")
	}
	sched.Tick(fireAt)

	evt := waitFire(t, sched)
	if evt.ID != task.ID {
		t.Fatalf("unexpected fired task: %+v", evt)
	}
	if len(store.List()) != 0 {
		t.Fatal("one-shot task should be removed after firing")
	}
}

func TestSchedulerAbsoluteTask(t *testing.T) {
	store := scheduler.NewStoreWithLocation(bj)
	sched := scheduler.NewScheduler(store)

	start := time.Date(2024, 1, 1, 0, 0, 0, 0, bj)
	at := time.Date(2024, 1, 2, 10, 30, 0, 0, bj)
	once, err := scheduler.NewOnce(at)
	if err != nil {
		t.Fatal(err)
	}
	task, err := store.AddAt("绝对时间提醒", once, scheduler.Source{}, start)
	if err != nil {
		t.Fatal(err)
	}
	sched.Tick(at.Add(-time.Minute))
	if len(store.List()) != 1 {
		t.Fatal("task should not fire before absolute time")
	}
	sched.Tick(at)
	if evt := waitFire(t, sched); evt.ID != task.ID {
		t.Fatalf("unexpected fired task: %+v", evt)
	}
	if len(store.List()) != 0 {
		t.Fatal("absolute task should be removed after firing")
	}
}

func waitFire(t *testing.T, sched *scheduler.Scheduler) scheduler.Task {
	t.Helper()
	select {
	case evt := <-sched.Fires():
		return evt
	case <-time.After(2 * time.Second):
		t.Fatal("expected a fire event")
		return scheduler.Task{}
	}
}
