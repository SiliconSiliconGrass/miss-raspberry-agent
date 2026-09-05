package todo_list_test

import (
	"context"
	"testing"

	"miss-raspberry-agent/internal/tools/todo_list"
)

func TestStoreAddListComplete(t *testing.T) {
	store := todo_list.NewStore()
	if !store.IsEmpty() {
		t.Fatal("new store should be empty")
	}

	first := store.Add(todo_list.Item{Content: "回复用户A", Source: "私聊(用户QQ=1)"})
	second := store.Add(todo_list.Item{Content: "回复群B", Source: "群聊(群号=2)"})

	if first.ID == "" || first.ID == second.ID {
		t.Fatalf("expected unique non-empty ids, got %q and %q", first.ID, second.ID)
	}
	if store.Len() != 2 || store.IsEmpty() {
		t.Fatalf("expected 2 items, len=%d empty=%v", store.Len(), store.IsEmpty())
	}

	items := store.List()
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if items[0].ID != first.ID || items[1].ID != second.ID {
		t.Errorf("expected FIFO order, got %+v", items)
	}

	deleted, ok := store.Complete(first.ID)
	if !ok || deleted.ID != first.ID {
		t.Fatalf("expected first item to be completed and deleted, got %+v ok=%v", deleted, ok)
	}
	if store.Len() != 1 || store.IsEmpty() {
		t.Fatalf("expected 1 remaining item, len=%d", store.Len())
	}
	if _, ok := store.Complete(first.ID); ok {
		t.Fatal("completing the same id twice should fail")
	}
}

// TestVersionIncrementsOnAdd verifies that Version is monotonic and reflects every Add, which
// polling consumers use to detect new queue items.
func TestVersionIncrementsOnAdd(t *testing.T) {
	store := todo_list.NewStore()
	if v := store.Version(); v != 0 {
		t.Fatalf("initial version = %d, want 0", v)
	}
	store.Add(todo_list.Item{Content: "one"})
	store.Add(todo_list.Item{Content: "two"})
	if v := store.Version(); v != 2 {
		t.Fatalf("version after two adds = %d, want 2", v)
	}
}

func TestTodoListToolActions(t *testing.T) {
	store := todo_list.NewStore()

	out, err := todo_list.RunTodoList(context.Background(), store, &todo_list.TodoListInput{Action: "add", Content: "任务一"})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if len(out.Items) != 1 {
		t.Fatalf("expected 1 item after add, got %+v", out.Items)
	}
	id := out.Items[0].ID

	out, err = todo_list.RunTodoList(context.Background(), store, &todo_list.TodoListInput{Action: "list"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(out.Items) != 1 || out.Items[0].ID != id {
		t.Fatalf("unexpected list result: %+v", out.Items)
	}

	out, err = todo_list.RunTodoList(context.Background(), store, &todo_list.TodoListInput{Action: "complete", ID: id})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if len(out.Items) != 0 {
		t.Fatalf("expected empty list after complete, got %+v", out.Items)
	}
	if !store.IsEmpty() {
		t.Fatal("store should be empty after complete")
	}
}

func TestTodoListToolValidation(t *testing.T) {
	store := todo_list.NewStore()

	cases := []struct {
		name string
		in   *todo_list.TodoListInput
	}{
		{"unknown action", &todo_list.TodoListInput{Action: "delete"}},
		{"add without content", &todo_list.TodoListInput{Action: "add"}},
		{"complete without id", &todo_list.TodoListInput{Action: "complete"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := todo_list.RunTodoList(context.Background(), store, tc.in); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}
