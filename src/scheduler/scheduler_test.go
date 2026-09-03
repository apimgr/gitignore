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
