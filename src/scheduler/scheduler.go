// Package scheduler implements the always-running, database-backed task
// scheduler mandated by AI.md PART 18. It parses cron/interval expressions with
// no external dependencies, persists task state to server.db, catches up missed
// runs on restart, and retries failed runs with exponential backoff.
package scheduler

import (
	"context"
	"errors"
	"log"
	"sort"
	"sync"
	"time"

	"github.com/apimgr/gitignore/src/db"
)

// ErrSkipped is returned by a task handler that intentionally did nothing
// (e.g. its subsystem is disabled). It is recorded as StatusSkipped, not a
// failure, and never counts against the retry budget.
var ErrSkipped = errors.New("task skipped")

// Task execution status values persisted to last_status (AI.md PART 18).
const (
	StatusPending = "pending"
	StatusRunning = "running"
	StatusSuccess = "success"
	StatusFailed  = "failed"
	StatusSkipped = "skipped"
)

// Default retry policy (AI.md PART 18 "Retry Policy").
const (
	defaultMaxRetries = 3
	defaultRetryDelay = 5 * time.Minute
	defaultTickEvery  = 30 * time.Second
	shutdownGrace     = 30 * time.Second
)

// HandlerFunc executes a scheduled task. Returning ErrSkipped records a skip;
// any other error records a failure and may trigger a retry.
type HandlerFunc func(ctx context.Context) error

// Task is a single registered scheduled task plus its live execution state.
type Task struct {
	ID        string
	Name      string
	Skippable bool
	Handler   HandlerFunc

	// Retry policy. RetryOnFail enables retries; zero values fall back to the
	// package defaults.
	RetryOnFail bool
	RetryDelay  time.Duration
	MaxRetries  int
	Backoff     bool

	scheduleExpr string
	schedule     Schedule

	// Guarded by Scheduler.mu.
	enabled    bool
	running    bool
	lastRun    time.Time
	nextRun    time.Time
	lastStatus string
	lastError  string
	runCount   int64
	failCount  int64
	lastDur    time.Duration
	attempt    int
}

// TaskInfo is an immutable snapshot of a task's state for reporting.
type TaskInfo struct {
	ID         string
	Name       string
	Schedule   string
	Enabled    bool
	Running    bool
	LastRun    time.Time
	NextRun    time.Time
	LastStatus string
	LastError  string
	LastDur    time.Duration
	RunCount   int64
	FailCount  int64
	Skippable  bool
}

// Config configures the scheduler at construction (AI.md PART 18 "Task
// Configuration").
type Config struct {
	// Timezone for cron schedules; defaults to America/New_York per spec.
	Timezone string
	// CatchUpWindow bounds how stale a missed run may be to still be caught up.
	CatchUpWindow time.Duration
	// TickInterval controls how often the loop checks for due tasks. Defaults
	// to 30s; exposed mainly for tests.
	TickInterval time.Duration
	// OnError, when set, is invoked (in a goroutine) whenever a task run fails.
	// It carries the task ID, human-readable name, the error, and the next
	// scheduled run time. Used by the wire layer to emit the scheduler_error
	// operator email (AI.md PART 17). Never called while holding the scheduler
	// lock.
	OnError func(id, name string, err error, nextRun time.Time)
}

// Scheduler owns the registered tasks and the background execution loop.
type Scheduler struct {
	mu       sync.Mutex
	tasks    map[string]*Task
	order    []string
	loc      *time.Location
	catchUp  time.Duration
	tick     time.Duration
	now      func() time.Time
	persist  func(db.SchedulerState) error
	onError  func(id, name string, err error, nextRun time.Time)

	stop    chan struct{}
	done    chan struct{}
	wg      sync.WaitGroup
	running bool
}

// New builds a scheduler from config. An unknown or empty timezone falls back
// to America/New_York, then UTC.
func New(cfg Config) *Scheduler {
	tz := cfg.Timezone
	if tz == "" {
		tz = "America/New_York"
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		log.Printf("scheduler: unknown timezone %q, falling back to UTC: %v", tz, err)
		loc = time.UTC
	}
	catchUp := cfg.CatchUpWindow
	if catchUp <= 0 {
		catchUp = time.Hour
	}
	tick := cfg.TickInterval
	if tick <= 0 {
		tick = defaultTickEvery
	}
	return &Scheduler{
		tasks:   make(map[string]*Task),
		loc:     loc,
		catchUp: catchUp,
		tick:    tick,
		now:     time.Now,
		persist: db.UpsertSchedulerState,
		onError: cfg.OnError,
	}
}

// Register adds a task. schedule must be a valid PART 18 expression. It is safe
// to call before Start; calling after Start also works but skips catch-up for
// that task.
func (s *Scheduler) Register(t *Task) error {
	sched, err := ParseSchedule(t.schedExpr())
	if err != nil {
		return err
	}
	t.schedule = sched
	t.enabled = true
	if t.lastStatus == "" {
		t.lastStatus = StatusPending
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.tasks[t.ID]; !exists {
		s.order = append(s.order, t.ID)
	}
	s.tasks[t.ID] = t
	return nil
}

// RegisterTask is a convenience wrapper for the common case.
func (s *Scheduler) RegisterTask(id, name, schedule string, skippable bool, h HandlerFunc) error {
	return s.Register(&Task{
		ID:           id,
		Name:         name,
		scheduleExpr: schedule,
		Skippable:    skippable,
		Handler:      h,
	})
}

func (t *Task) schedExpr() string { return t.scheduleExpr }

// Start loads persisted state, catches up missed runs, and launches the loop.
func (s *Scheduler) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return nil
	}

	s.loadState()
	now := s.now().In(s.loc)
	var catchUp []*Task
	for _, id := range s.order {
		t := s.tasks[id]
		if t.nextRun.IsZero() {
			t.nextRun = t.schedule.Next(s.baseTime(t, now))
		}
		if s.shouldCatchUp(t, now) {
			catchUp = append(catchUp, t)
		}
	}
	// Run missed tasks in order of their original scheduled time.
	sort.SliceStable(catchUp, func(i, j int) bool {
		return catchUp[i].nextRun.Before(catchUp[j].nextRun)
	})
	for _, id := range s.order {
		s.saveTaskLocked(s.tasks[id])
	}
	s.running = true
	s.stop = make(chan struct{})
	s.done = make(chan struct{})
	tasks := append([]*Task(nil), catchUp...)
	s.mu.Unlock()

	for _, t := range tasks {
		log.Printf("scheduler: catching up missed task %q", t.ID)
		s.execute(ctx, t, true)
	}

	go s.loop(ctx)
	log.Printf("scheduler: started with %d tasks (tz=%s, catch-up=%s)", len(s.order), s.loc, s.catchUp)
	return nil
}

// shouldCatchUp reports whether a task's most recent scheduled fire was missed
// while the process was down and is still within the catch-up window.
func (s *Scheduler) shouldCatchUp(t *Task, now time.Time) bool {
	if !t.enabled || t.lastRun.IsZero() {
		return false
	}
	missed := t.schedule.Next(t.lastRun.In(s.loc))
	if missed.IsZero() || !missed.Before(now) {
		return false
	}
	return now.Sub(missed) <= s.catchUp
}

// baseTime picks the anchor for computing a task's next run: its last run if
// known, otherwise now.
func (s *Scheduler) baseTime(t *Task, now time.Time) time.Time {
	if !t.lastRun.IsZero() {
		return t.lastRun.In(s.loc)
	}
	return now
}

// Stop signals the loop, waits for running tasks (bounded), then persists state.
func (s *Scheduler) Stop(ctx context.Context) {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	s.running = false
	close(s.stop)
	done := s.done
	s.mu.Unlock()

	// Wait for the loop goroutine to exit.
	select {
	case <-done:
	case <-time.After(shutdownGrace):
		log.Println("scheduler: shutdown grace expired waiting for loop")
	}

	// Wait for in-flight task handlers, bounded by the grace window.
	waitCh := make(chan struct{})
	go func() { s.wg.Wait(); close(waitCh) }()
	select {
	case <-waitCh:
	case <-time.After(shutdownGrace):
		log.Println("scheduler: shutdown grace expired; some tasks marked for retry on restart")
	}

	s.mu.Lock()
	for _, id := range s.order {
		s.saveTaskLocked(s.tasks[id])
	}
	s.mu.Unlock()
	log.Println("scheduler: stopped")
}

func (s *Scheduler) loop(ctx context.Context) {
	ticker := time.NewTicker(s.tick)
	defer ticker.Stop()
	defer close(s.done)
	for {
		select {
		case <-s.stop:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runDue(ctx)
		}
	}
}

// runDue launches every enabled, due, not-already-running task.
func (s *Scheduler) runDue(ctx context.Context) {
	now := s.now().In(s.loc)
	s.mu.Lock()
	var due []*Task
	for _, id := range s.order {
		t := s.tasks[id]
		if t.enabled && !t.running && !t.nextRun.IsZero() && !now.Before(t.nextRun) {
			t.running = true
			due = append(due, t)
		}
	}
	s.mu.Unlock()

	for _, t := range due {
		s.wg.Add(1)
		go func(t *Task) {
			defer s.wg.Done()
			s.execute(ctx, t, false)
		}(t)
	}
}

// execute runs a task's handler and records the outcome. markRunning is false
// when the caller already set t.running (the async path).
func (s *Scheduler) execute(ctx context.Context, t *Task, markRunning bool) {
	s.mu.Lock()
	if markRunning {
		if t.running {
			s.mu.Unlock()
			return
		}
		t.running = true
	}
	handler := t.Handler
	s.mu.Unlock()

	start := s.now()
	var err error
	if handler != nil {
		err = handler(ctx)
	}
	dur := s.now().Sub(start)

	s.mu.Lock()
	defer s.mu.Unlock()
	t.running = false
	t.lastRun = start
	t.lastDur = dur

	switch {
	case err == nil:
		t.lastStatus = StatusSuccess
		t.lastError = ""
		t.runCount++
		t.attempt = 0
		t.nextRun = t.schedule.Next(start.In(s.loc))
		log.Printf("scheduler: task %q succeeded in %s", t.ID, dur.Round(time.Millisecond))
	case errors.Is(err, ErrSkipped):
		t.lastStatus = StatusSkipped
		t.lastError = ""
		t.attempt = 0
		t.nextRun = t.schedule.Next(start.In(s.loc))
		log.Printf("scheduler: task %q skipped", t.ID)
	default:
		t.lastStatus = StatusFailed
		t.lastError = err.Error()
		t.failCount++
		s.scheduleRetryLocked(t, start)
		log.Printf("scheduler: task %q failed: %v", t.ID, err)
		if s.onError != nil {
			// Run outside the scheduler lock: emitting an operator email must
			// never block or deadlock the execution loop.
			go s.onError(t.ID, t.Name, err, t.nextRun)
		}
	}
	s.saveTaskLocked(t)
}

// scheduleRetryLocked sets nextRun for a retry with exponential backoff, or the
// normal next run once retries are exhausted. Caller holds s.mu.
func (s *Scheduler) scheduleRetryLocked(t *Task, ref time.Time) {
	if !t.RetryOnFail {
		t.attempt = 0
		t.nextRun = t.schedule.Next(ref.In(s.loc))
		return
	}
	maxRetries := t.MaxRetries
	if maxRetries <= 0 {
		maxRetries = defaultMaxRetries
	}
	if t.attempt >= maxRetries {
		t.attempt = 0
		t.nextRun = t.schedule.Next(ref.In(s.loc))
		return
	}
	delay := t.RetryDelay
	if delay <= 0 {
		delay = defaultRetryDelay
	}
	if t.Backoff {
		delay *= 1 << uint(t.attempt)
	}
	t.attempt++
	t.nextRun = ref.Add(delay)
	log.Printf("scheduler: task %q retry %d/%d in %s", t.ID, t.attempt, maxRetries, delay)
}

// RunNow executes a task synchronously, bypassing the schedule but honoring the
// enabled flag only for logging. Returns the handler error.
func (s *Scheduler) RunNow(ctx context.Context, id string) error {
	s.mu.Lock()
	t, ok := s.tasks[id]
	s.mu.Unlock()
	if !ok {
		return errors.New("unknown task: " + id)
	}
	s.execute(ctx, t, true)
	s.mu.Lock()
	defer s.mu.Unlock()
	if t.lastStatus == StatusFailed {
		return errors.New(t.lastError)
	}
	return nil
}

// SetEnabled toggles a task and persists the change.
func (s *Scheduler) SetEnabled(id string, enabled bool) error {
	s.mu.Lock()
	t, ok := s.tasks[id]
	if !ok {
		s.mu.Unlock()
		return errors.New("unknown task: " + id)
	}
	t.enabled = enabled
	if enabled && t.nextRun.IsZero() {
		t.nextRun = t.schedule.Next(s.now().In(s.loc))
	}
	s.saveTaskLocked(t)
	s.mu.Unlock()
	return nil
}

// Tasks returns a snapshot of all tasks in registration order.
func (s *Scheduler) Tasks() []TaskInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]TaskInfo, 0, len(s.order))
	for _, id := range s.order {
		out = append(out, s.tasks[id].info())
	}
	return out
}

// Task returns a snapshot of one task, or false if it is not registered.
func (s *Scheduler) Task(id string) (TaskInfo, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok {
		return TaskInfo{}, false
	}
	return t.info(), true
}

func (t *Task) info() TaskInfo {
	status := t.lastStatus
	if t.running {
		status = StatusRunning
	}
	return TaskInfo{
		ID:         t.ID,
		Name:       t.Name,
		Schedule:   t.scheduleExpr,
		Enabled:    t.enabled,
		Running:    t.running,
		LastRun:    t.lastRun,
		NextRun:    t.nextRun,
		LastStatus: status,
		LastError:  t.lastError,
		LastDur:    t.lastDur,
		RunCount:   t.runCount,
		FailCount:  t.failCount,
		Skippable:  t.Skippable,
	}
}

// LoadPersisted merges persisted DB state into registered tasks without
// starting the loop. It is used by the CLI to inspect task state.
func (s *Scheduler) LoadPersisted() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadState()
	now := s.now().In(s.loc)
	for _, id := range s.order {
		t := s.tasks[id]
		if t.nextRun.IsZero() {
			t.nextRun = t.schedule.Next(s.baseTime(t, now))
		}
	}
}

// loadState merges persisted state into registered tasks. Caller holds s.mu.
func (s *Scheduler) loadState() {
	states, err := db.LoadSchedulerStates()
	if err != nil {
		log.Printf("scheduler: failed to load persisted state: %v", err)
		return
	}
	for _, st := range states {
		t, ok := s.tasks[st.TaskID]
		if !ok {
			continue
		}
		t.enabled = st.Enabled
		t.lastRun = st.LastRun
		t.lastStatus = st.LastStatus
		t.lastError = st.LastError
		t.runCount = st.RunCount
		t.failCount = st.FailCount
		if !st.NextRun.IsZero() {
			t.nextRun = st.NextRun
		}
	}
}

// saveTaskLocked persists one task's state. Caller holds s.mu.
func (s *Scheduler) saveTaskLocked(t *Task) {
	if s.persist == nil {
		return
	}
	if err := s.persist(db.SchedulerState{
		TaskID:     t.ID,
		TaskName:   t.Name,
		Schedule:   t.scheduleExpr,
		LastRun:    t.lastRun,
		LastStatus: t.lastStatus,
		LastError:  t.lastError,
		NextRun:    t.nextRun,
		RunCount:   t.runCount,
		FailCount:  t.failCount,
		Enabled:    t.enabled,
	}); err != nil {
		log.Printf("scheduler: failed to persist task %q: %v", t.ID, err)
	}
}
