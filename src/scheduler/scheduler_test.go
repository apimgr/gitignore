package scheduler

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/apimgr/gitignore/src/db"
)

// newTestScheduler builds a scheduler with an in-memory persist sink and a
// controllable clock, avoiding any database dependency.
func newTestScheduler() (*Scheduler, *sync.Map) {
	s := New(Config{Timezone: "UTC", CatchUpWindow: time.Hour})
	var store sync.Map
	s.persist = func(st db.SchedulerState) error {
		store.Store(st.TaskID, st)
		return nil
	}
	return s, &store
}

func TestRegisterInvalidSchedule(t *testing.T) {
	s, _ := newTestScheduler()
	if err := s.RegisterTask("bad", "Bad", "not a cron", false, nil); err == nil {
		t.Fatal("expected error for invalid schedule")
	}
}

func TestExecuteSuccess(t *testing.T) {
	s, store := newTestScheduler()
	ran := false
	if err := s.RegisterTask("t1", "T1", "@every 1h", false, func(ctx context.Context) error {
		ran = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	s.execute(context.Background(), s.tasks["t1"], true)
	if !ran {
		t.Fatal("handler did not run")
	}
	info, _ := s.Task("t1")
	if info.LastStatus != StatusSuccess {
		t.Errorf("status = %q, want success", info.LastStatus)
	}
	if info.RunCount != 1 {
		t.Errorf("run count = %d, want 1", info.RunCount)
	}
	if _, ok := store.Load("t1"); !ok {
		t.Error("state was not persisted")
	}
}

func TestExecuteSkipped(t *testing.T) {
	s, _ := newTestScheduler()
	_ = s.RegisterTask("t2", "T2", "@every 1h", true, func(ctx context.Context) error {
		return ErrSkipped
	})
	s.execute(context.Background(), s.tasks["t2"], true)
	info, _ := s.Task("t2")
	if info.LastStatus != StatusSkipped {
		t.Errorf("status = %q, want skipped", info.LastStatus)
	}
	if info.FailCount != 0 {
		t.Errorf("skip must not count as failure, got fail_count=%d", info.FailCount)
	}
}

func TestExecuteFailureAndRetryBackoff(t *testing.T) {
	s, _ := newTestScheduler()
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return base }
	task := &Task{
		ID:           "retry",
		Name:         "Retry",
		scheduleExpr: "0 0 * * *",
		Handler:      func(ctx context.Context) error { return errors.New("boom") },
		RetryOnFail:  true,
		RetryDelay:   5 * time.Minute,
		MaxRetries:   3,
		Backoff:      true,
	}
	if err := s.Register(task); err != nil {
		t.Fatal(err)
	}

	s.execute(context.Background(), task, true)
	info, _ := s.Task("retry")
	if info.LastStatus != StatusFailed {
		t.Fatalf("status = %q, want failed", info.LastStatus)
	}
	if info.FailCount != 1 {
		t.Errorf("fail count = %d, want 1", info.FailCount)
	}
	// First retry: 5m after now.
	if got, want := info.NextRun, base.Add(5*time.Minute); !got.Equal(want) {
		t.Errorf("retry 1 next = %v, want %v", got, want)
	}

	s.execute(context.Background(), task, true)
	info, _ = s.Task("retry")
	// Second retry doubles the delay: 10m.
	if got, want := info.NextRun, base.Add(10*time.Minute); !got.Equal(want) {
		t.Errorf("retry 2 next = %v, want %v", got, want)
	}
}

func TestSetEnabled(t *testing.T) {
	s, store := newTestScheduler()
	_ = s.RegisterTask("t3", "T3", "@every 1h", false, func(ctx context.Context) error { return nil })
	if err := s.SetEnabled("t3", false); err != nil {
		t.Fatal(err)
	}
	info, _ := s.Task("t3")
	if info.Enabled {
		t.Error("task should be disabled")
	}
	st, ok := store.Load("t3")
	if !ok || st.(db.SchedulerState).Enabled {
		t.Error("disabled state not persisted")
	}
}

func TestSetEnabledUnknown(t *testing.T) {
	s, _ := newTestScheduler()
	if err := s.SetEnabled("nope", true); err == nil {
		t.Error("expected error for unknown task")
	}
}

func TestRunNowFailure(t *testing.T) {
	s, _ := newTestScheduler()
	_ = s.RegisterTask("t4", "T4", "@every 1h", false, func(ctx context.Context) error {
		return errors.New("nope")
	})
	if err := s.RunNow(context.Background(), "t4"); err == nil {
		t.Error("expected error from failing task")
	}
}

func TestTasksOrder(t *testing.T) {
	s, _ := newTestScheduler()
	for _, id := range []string{"a", "b", "c"} {
		_ = s.RegisterTask(id, id, "@every 1h", false, nil)
	}
	tasks := s.Tasks()
	if len(tasks) != 3 || tasks[0].ID != "a" || tasks[2].ID != "c" {
		t.Errorf("registration order not preserved: %+v", tasks)
	}
}

func TestTaskUnknown(t *testing.T) {
	s, _ := newTestScheduler()
	if _, ok := s.Task("nope"); ok {
		t.Error("expected ok=false for unknown task")
	}
}

func TestRunNowUnknownTask(t *testing.T) {
	s, _ := newTestScheduler()
	if err := s.RunNow(context.Background(), "nope"); err == nil {
		t.Error("expected error for unknown task")
	}
}

func TestRunNowSuccess(t *testing.T) {
	s, _ := newTestScheduler()
	_ = s.RegisterTask("ok", "OK", "@every 1h", false, func(ctx context.Context) error { return nil })
	if err := s.RunNow(context.Background(), "ok"); err != nil {
		t.Errorf("RunNow: %v", err)
	}
	info, _ := s.Task("ok")
	if info.LastStatus != StatusSuccess {
		t.Errorf("status = %q, want success", info.LastStatus)
	}
}

// re-registering the same ID must not duplicate it in the order slice.
func TestRegisterSameIDTwiceDoesNotDuplicateOrder(t *testing.T) {
	s, _ := newTestScheduler()
	_ = s.RegisterTask("dup", "Dup", "@every 1h", false, nil)
	_ = s.RegisterTask("dup", "Dup Renamed", "@every 2h", false, nil)
	tasks := s.Tasks()
	if len(tasks) != 1 {
		t.Fatalf("got %d tasks, want 1: %+v", len(tasks), tasks)
	}
	if tasks[0].Name != "Dup Renamed" {
		t.Errorf("Name = %q, want %q", tasks[0].Name, "Dup Renamed")
	}
}

func TestInfoReflectsRunningStatus(t *testing.T) {
	s, _ := newTestScheduler()
	_ = s.RegisterTask("run", "Run", "@every 1h", false, nil)
	s.mu.Lock()
	s.tasks["run"].running = true
	s.mu.Unlock()
	info, _ := s.Task("run")
	if info.LastStatus != StatusRunning {
		t.Errorf("LastStatus = %q, want %q", info.LastStatus, StatusRunning)
	}
	if !info.Running {
		t.Error("Running = false, want true")
	}
}

func TestStartAndStop(t *testing.T) {
	s, store := newTestScheduler()
	s.tick = 10 * time.Millisecond
	ran := make(chan struct{}, 1)
	_ = s.RegisterTask("loop1", "Loop1", "@every 1ms", false, func(ctx context.Context) error {
		select {
		case ran <- struct{}{}:
		default:
		}
		return nil
	})

	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	// Starting a second time must be a no-op, not a panic or double-loop.
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("second Start: %v", err)
	}

	select {
	case <-ran:
	case <-time.After(2 * time.Second):
		t.Fatal("task never ran after Start")
	}

	s.Stop(context.Background())
	// Stopping again must be a no-op, not a panic on a closed channel.
	s.Stop(context.Background())

	if _, ok := store.Load("loop1"); !ok {
		t.Error("state was not persisted on stop")
	}
}

func TestStartCatchesUpMissedTask(t *testing.T) {
	s, _ := newTestScheduler()
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return base }
	s.tick = time.Hour

	task := &Task{
		ID:           "missed",
		Name:         "Missed",
		scheduleExpr: "@every 1h",
	}
	if err := s.Register(task); err != nil {
		t.Fatal(err)
	}
	// Simulate a task that last ran two hours ago, so its next fire (one hour
	// ago) was missed while the process was down but is still inside the
	// one-hour catch-up window boundary check below.
	s.mu.Lock()
	task.lastRun = base.Add(-90 * time.Minute)
	s.catchUp = 2 * time.Hour
	s.mu.Unlock()

	caught := make(chan struct{}, 1)
	task.Handler = func(ctx context.Context) error {
		select {
		case caught <- struct{}{}:
		default:
		}
		return nil
	}

	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop(context.Background())

	select {
	case <-caught:
	case <-time.After(2 * time.Second):
		t.Fatal("missed task was not caught up on Start")
	}
}

func TestShouldCatchUp(t *testing.T) {
	s, _ := newTestScheduler()
	now := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)

	disabled := &Task{scheduleExpr: "@every 1h"}
	disabled.schedule, _ = ParseSchedule(disabled.scheduleExpr)
	disabled.lastRun = now.Add(-2 * time.Hour)
	if s.shouldCatchUp(disabled, now) {
		t.Error("disabled task must never catch up")
	}

	neverRun := &Task{scheduleExpr: "@every 1h", enabled: true}
	neverRun.schedule, _ = ParseSchedule(neverRun.scheduleExpr)
	if s.shouldCatchUp(neverRun, now) {
		t.Error("task with no lastRun must never catch up")
	}

	tooOld := &Task{scheduleExpr: "@every 1h", enabled: true}
	tooOld.schedule, _ = ParseSchedule(tooOld.scheduleExpr)
	tooOld.lastRun = now.Add(-10 * time.Hour)
	if s.shouldCatchUp(tooOld, now) {
		t.Error("missed run outside the catch-up window must not catch up")
	}

	within := &Task{scheduleExpr: "@every 1h", enabled: true}
	within.schedule, _ = ParseSchedule(within.scheduleExpr)
	within.lastRun = now.Add(-90 * time.Minute)
	if !s.shouldCatchUp(within, now) {
		t.Error("missed run inside the catch-up window should catch up")
	}
}

func TestLoadPersistedMergesState(t *testing.T) {
	dataDir := t.TempDir()
	if err := db.Init(dataDir); err != nil {
		t.Fatalf("db.Init: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	loadedNextRun := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	if err := db.UpsertSchedulerState(db.SchedulerState{
		TaskID:     "p1",
		TaskName:   "P1",
		Schedule:   "@every 1h",
		Enabled:    false,
		LastStatus: StatusFailed,
		RunCount:   4,
		FailCount:  2,
		NextRun:    loadedNextRun,
	}); err != nil {
		t.Fatalf("UpsertSchedulerState: %v", err)
	}

	s := New(Config{Timezone: "UTC", CatchUpWindow: time.Hour})
	_ = s.RegisterTask("p1", "P1", "@every 1h", false, nil)

	s.LoadPersisted()

	info, ok := s.Task("p1")
	if !ok {
		t.Fatal("p1 not registered")
	}
	if info.Enabled {
		t.Error("Enabled should have been overwritten by persisted state (false)")
	}
	if info.LastStatus != StatusFailed {
		t.Errorf("LastStatus = %q, want %q", info.LastStatus, StatusFailed)
	}
	if info.RunCount != 4 || info.FailCount != 2 {
		t.Errorf("RunCount/FailCount = %d/%d, want 4/2", info.RunCount, info.FailCount)
	}
	if !info.NextRun.Equal(loadedNextRun) {
		t.Errorf("NextRun = %v, want %v", info.NextRun, loadedNextRun)
	}
}

func TestRunDueSkipsNotYetDueAndAlreadyRunning(t *testing.T) {
	s, _ := newTestScheduler()
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return base }

	due := &Task{ID: "due", scheduleExpr: "@every 1h", enabled: true}
	due.schedule, _ = ParseSchedule(due.scheduleExpr)
	due.nextRun = base.Add(-time.Minute)
	var ranDue bool
	due.Handler = func(ctx context.Context) error { ranDue = true; return nil }

	notDue := &Task{ID: "notdue", scheduleExpr: "@every 1h", enabled: true}
	notDue.schedule, _ = ParseSchedule(notDue.scheduleExpr)
	notDue.nextRun = base.Add(time.Hour)
	var ranNotDue bool
	notDue.Handler = func(ctx context.Context) error { ranNotDue = true; return nil }

	alreadyRunning := &Task{ID: "running", scheduleExpr: "@every 1h", enabled: true, running: true}
	alreadyRunning.schedule, _ = ParseSchedule(alreadyRunning.scheduleExpr)
	alreadyRunning.nextRun = base.Add(-time.Minute)

	s.mu.Lock()
	for _, tk := range []*Task{due, notDue, alreadyRunning} {
		s.tasks[tk.ID] = tk
		s.order = append(s.order, tk.ID)
	}
	s.mu.Unlock()

	s.runDue(context.Background())
	s.wg.Wait()

	if !ranDue {
		t.Error("due task should have run")
	}
	if ranNotDue {
		t.Error("not-yet-due task should not have run")
	}
}
