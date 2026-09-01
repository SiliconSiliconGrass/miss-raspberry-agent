package scheduler

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
	_ "time/tzdata" // 保证 Asia/Shanghai 时区数据可用
)

// Source 记录创建定时任务时的消息来源，触发后作为回复目标。
type Source struct {
	Description string
	TargetType  string
	TargetID    int64
	UserID      int64
}

// Task 是一条定时任务。
type Task struct {
	ID         string `json:"id"`
	Content    string `json:"content"`
	Schedule   string `json:"schedule"`
	Kind       Kind   `json:"kind"`
	Repeat     bool   `json:"repeat"`
	NextRunAt  int64  `json:"next_run_at"`
	Source     string `json:"source,omitempty"`
	TargetType string `json:"target_type,omitempty"`
	TargetID   int64  `json:"target_id,omitempty"`
	UserID     int64  `json:"user_id,omitempty"`
	CreatedAt  int64  `json:"created_at"`

	sch *Schedule
	loc *time.Location
}

// NextRunText 返回下一次触发时间的北京时间文本。
func (t *Task) NextRunText() string {
	if t.NextRunAt <= 0 || t.loc == nil {
		return "-"
	}
	return time.Unix(t.NextRunAt, 0).In(t.loc).Format("2006-01-02 15:04:05")
}

// BeijingLocation 返回 Asia/Shanghai 时区。
func BeijingLocation() *time.Location {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.FixedZone("Asia/Shanghai", 8*3600)
	}
	return loc
}

// Store 是线程安全的定时任务内存存储。
type Store struct {
	mu     sync.Mutex
	tasks  map[string]*Task
	nextID int64
	loc    *time.Location
}

// NewStore 创建一个使用北京时间的任务存储。
func NewStore() *Store {
	return NewStoreWithLocation(BeijingLocation())
}

// NewStoreWithLocation 创建一个使用指定时区的任务存储（供测试等场景使用）。
func NewStoreWithLocation(loc *time.Location) *Store {
	if loc == nil {
		loc = BeijingLocation()
	}
	return &Store{tasks: map[string]*Task{}, loc: loc}
}

// Add 使用给定的调度规则创建定时任务。单次任务的目标时间必须晚于当前时间。
func (s *Store) Add(content string, sch *Schedule, src Source) (*Task, error) {
	return s.AddAt(content, sch, src, time.Now().In(s.loc))
}

// AddAt 与 Add 相同，但使用指定的当前时间计算首次触发时间（供测试注入时钟）。
func (s *Store) AddAt(content string, sch *Schedule, src Source, now time.Time) (*Task, error) {
	if sch == nil {
		return nil, errors.New("schedule is required")
	}
	now = now.In(s.loc)
	next, ok := sch.Next(now)
	if !ok {
		return nil, fmt.Errorf("触发时间已过期")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	t := &Task{
		ID:         fmt.Sprintf("task-%d", s.nextID),
		Content:    content,
		Schedule:   sch.Describe(),
		Kind:       sch.Kind,
		Repeat:     sch.Repeat(),
		NextRunAt:  next.Unix(),
		Source:     src.Description,
		TargetType: src.TargetType,
		TargetID:   src.TargetID,
		UserID:     src.UserID,
		CreatedAt:  now.Unix(),
		sch:        sch,
		loc:        s.loc,
	}
	s.tasks[t.ID] = t
	copy := copyTask(t)
	return &copy, nil
}

// List 返回全部任务，按下次触发时间从早到晚排列。
func (s *Store) List() []Task {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Task, 0, len(s.tasks))
	for _, t := range s.tasks {
		out = append(out, copyTask(t))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].NextRunAt != out[j].NextRunAt {
			return out[i].NextRunAt < out[j].NextRunAt
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// Cancel 删除指定任务。
func (s *Store) Cancel(id string) (Task, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok {
		return Task{}, false
	}
	delete(s.tasks, id)
	return copyTask(t), true
}

// Location 返回任务存储使用的时区。
func (s *Store) Location() *time.Location {
	return s.loc
}

// due 返回所有到点任务（按触发时间排序）。仅由 Scheduler 的调度协程调用。
func (s *Store) due(now time.Time) []*Task {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*Task
	for _, t := range s.tasks {
		if t.NextRunAt <= now.Unix() {
			out = append(out, t)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].NextRunAt < out[j].NextRunAt })
	return out
}

func copyTask(t *Task) Task {
	return *t // 浅拷贝；sch 只读共享
}

// Scheduler 周期检查任务，到点后把触发事件发到 Fires 通道。
// 事件发送成功后才推进任务状态，避免任务在通道拥塞时丢失。
type Scheduler struct {
	store *Store
	fires chan Task
}

// NewScheduler 创建调度器。
func NewScheduler(store *Store) *Scheduler {
	return &Scheduler{store: store, fires: make(chan Task, 256)}
}

// Store 返回底层任务存储。
func (s *Scheduler) Store() *Store {
	return s.store
}

// Fires 返回触发事件通道；每个事件是一次到点任务的副本。
func (s *Scheduler) Fires() <-chan Task {
	return s.fires
}

// Run 阻塞运行调度循环（每秒检查一次），直到 ctx 取消。
func (s *Scheduler) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			if !s.dispatch(ctx, now) {
				return
			}
		}
	}
}

// Tick 手动触发一次调度检查（供测试使用）。
func (s *Scheduler) Tick(now time.Time) {
	s.dispatch(context.Background(), now)
}

// dispatch 触发所有到点任务：单次任务删除，重复任务推进到下一次触发时间。
func (s *Scheduler) dispatch(ctx context.Context, now time.Time) bool {
	for _, t := range s.store.due(now) {
		s.store.mu.Lock()
		cur, exists := s.store.tasks[t.ID]
		if !exists || cur.NextRunAt > now.Unix() {
			s.store.mu.Unlock()
			continue
		}
		evt := copyTask(cur)
		next, ok := cur.sch.Next(now.In(cur.loc))
		if !cur.Repeat || !ok {
			delete(s.store.tasks, cur.ID)
		} else {
			cur.NextRunAt = next.Unix()
		}
		s.store.mu.Unlock()

		select {
		case s.fires <- evt:
		case <-ctx.Done():
			return false
		}
	}
	return true
}
