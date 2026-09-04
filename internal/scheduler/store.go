package scheduler

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
	_ "time/tzdata" // ensures the Asia/Shanghai timezone data is available
)

// Source records where the scheduled task was created from; once fired it is
// the reply target.
type Source struct {
	Description string
	TargetType  string
	TargetID    int64
	UserID      int64
}

// Task is a single scheduled task.
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

// NextRunText returns the next firing time as Beijing-time text.
func (t *Task) NextRunText() string {
	if t.NextRunAt <= 0 || t.loc == nil {
		return "-"
	}
	return time.Unix(t.NextRunAt, 0).In(t.loc).Format("2006-01-02 15:04:05")
}

// BeijingLocation returns the Asia/Shanghai timezone.
func BeijingLocation() *time.Location {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.FixedZone("Asia/Shanghai", 8*3600)
	}
	return loc
}

// Store is a thread-safe in-memory store of scheduled tasks.
type Store struct {
	mu     sync.Mutex
	tasks  map[string]*Task
	nextID int64
	loc    *time.Location
}

// NewStore creates a task store that uses Beijing time.
func NewStore() *Store {
	return NewStoreWithLocation(BeijingLocation())
}

// NewStoreWithLocation creates a task store using the given timezone (e.g. for
// tests).
func NewStoreWithLocation(loc *time.Location) *Store {
	if loc == nil {
		loc = BeijingLocation()
	}
	return &Store{tasks: map[string]*Task{}, loc: loc}
}

// Add creates a scheduled task from the given schedule. A one-time task's
// target time must be later than the current time.
func (s *Store) Add(content string, sch *Schedule, src Source) (*Task, error) {
	return s.AddAt(content, sch, src, time.Now().In(s.loc))
}

// AddAt is like Add but computes the first firing time from the given current
// time (for injecting a clock in tests).
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

// List returns all tasks ordered by next firing time from earliest to latest.
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

// Cancel deletes the task with the given id.
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

// Location returns the timezone used by the task store.
func (s *Store) Location() *time.Location {
	return s.loc
}

// due returns all due tasks (ordered by firing time). It is only called by the
// Scheduler's dispatch goroutine.
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
	return *t // shallow copy; sch is shared read-only
}

// Scheduler periodically checks tasks and sends a firing event on the Fires
// channel when a task is due.
// Task state only advances after the event has been sent successfully, so no
// task is lost when the channel is congested.
type Scheduler struct {
	store *Store
	fires chan Task
}

// NewScheduler creates a scheduler.
func NewScheduler(store *Store) *Scheduler {
	return &Scheduler{store: store, fires: make(chan Task, 256)}
}

// Store returns the underlying task store.
func (s *Scheduler) Store() *Store {
	return s.store
}

// Fires returns the firing-event channel; each event is a copy of a due task.
func (s *Scheduler) Fires() <-chan Task {
	return s.fires
}

// Run blocks running the dispatch loop (checking once per second) until ctx is
// cancelled.
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

// Tick manually triggers one dispatch check (for tests).
func (s *Scheduler) Tick(now time.Time) {
	s.dispatch(context.Background(), now)
}

// dispatch fires all due tasks: one-time tasks are deleted and repeating tasks
// advance to their next firing time.
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
