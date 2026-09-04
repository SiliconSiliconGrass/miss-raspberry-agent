// Package todo_list provides the todo list storage and tools used by the agent.
// This list is only for recording tasks that need immediate handling, not for long-term reminders.
package todo_list

import (
	"fmt"
	"sort"
	"sync"
	"time"
)

// Item is a single todo entry.
// When a todo comes from a QQ message, Source/TargetType/TargetID/UserID
// record the message source so the agent can reply using qq_message_sender.
type Item struct {
	ID         string `json:"id"`
	Content    string `json:"content"`
	Source     string `json:"source,omitempty"`      // human-readable description of the message source
	TargetType string `json:"target_type,omitempty"` // private=private chat, group=group chat
	TargetID   int64  `json:"target_id,omitempty"`   // reply target: the user QQ number for private chat, the group number for group chat
	UserID     int64  `json:"user_id,omitempty"`     // QQ number of the message sender
	CreatedAt  int64  `json:"created_at"`
}

// Store is an in-memory thread-safe todo list.
type Store struct {
	mu     sync.RWMutex
	items  map[string]Item
	nextID int64
}

// NewStore creates an empty todo list.
func NewStore() *Store {
	return &Store{items: make(map[string]Item)}
}

// Add adds a todo and returns the entry with its ID. When CreatedAt is zero, it is set to the current time.
func (s *Store) Add(item Item) Item {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.nextID++
	item.ID = fmt.Sprintf("item-%d", s.nextID)
	if item.CreatedAt == 0 {
		item.CreatedAt = time.Now().Unix()
	}
	s.items[item.ID] = item
	return item
}

// List returns all todos ordered by creation time from earliest to latest.
func (s *Store) List() []Item {
	s.mu.RLock()
	defer s.mu.RUnlock()

	items := make([]Item, 0, len(s.items))
	for _, item := range s.items {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt != items[j].CreatedAt {
			return items[i].CreatedAt < items[j].CreatedAt
		}
		return items[i].ID < items[j].ID
	})
	return items
}

// Complete marks the specified todo as done and removes it immediately; it returns the removed entry.
func (s *Store) Complete(id string) (Item, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	item, ok := s.items[id]
	if ok {
		delete(s.items, id)
	}
	return item, ok
}

// IsEmpty reports whether the todo list is empty.
func (s *Store) IsEmpty() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.items) == 0
}

// Len returns the number of todos.
func (s *Store) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.items)
}
