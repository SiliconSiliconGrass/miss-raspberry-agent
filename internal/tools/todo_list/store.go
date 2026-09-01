// Package todo_list 提供 agent 使用的待办列表存储与工具。
// 该列表只用于记录需要立即处理的任务，不用于长期提醒。
package todo_list

import (
	"fmt"
	"sort"
	"sync"
	"time"
)

// Item 是一条待办事项。
// 当待办来自一条 QQ 消息时，Source/TargetType/TargetID/UserID
// 记录了消息来源，方便 agent 用 qq_message_sender 回复。
type Item struct {
	ID         string `json:"id"`
	Content    string `json:"content"`
	Source     string `json:"source,omitempty"`      // 可读的消息来源描述
	TargetType string `json:"target_type,omitempty"` // private=私聊，group=群聊
	TargetID   int64  `json:"target_id,omitempty"`   // 回复目标：私聊为用户QQ号，群聊为群号
	UserID     int64  `json:"user_id,omitempty"`     // 消息发送者QQ号
	CreatedAt  int64  `json:"created_at"`
}

// Store 是内存中的线程安全待办列表。
type Store struct {
	mu     sync.RWMutex
	items  map[string]Item
	nextID int64
}

// NewStore 创建一个空的待办列表。
func NewStore() *Store {
	return &Store{items: make(map[string]Item)}
}

// Add 添加一条待办并返回带 ID 的条目。CreatedAt 为空时取当前时间。
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

// List 返回全部待办，按创建时间从早到晚排列。
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

// Complete 标记指定待办为已完成并立即删除；返回被删除的条目。
func (s *Store) Complete(id string) (Item, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	item, ok := s.items[id]
	if ok {
		delete(s.items, id)
	}
	return item, ok
}

// IsEmpty 报告待办列表是否为空。
func (s *Store) IsEmpty() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.items) == 0
}

// Len 返回待办数量。
func (s *Store) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.items)
}
