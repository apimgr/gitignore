package scheduler

import (
	"testing"
	"time"
)

func mustParse(t *testing.T, expr string) Schedule {
	t.Helper()
	s, err := ParseSchedule(expr)
	if err != nil {
		t.Fatalf("ParseSchedule(%q) error: %v", expr, err)
	}
	return s
}

func TestParseScheduleErrors(t *testing.T) {
	for _, expr := range []string{"", "   ", "@every", "@every 0s", "@every -1m", "* * * *", "60 * * * *", "* 24 * * *", "foo * * * *", "@bogus"} {
		if _, err := ParseSchedule(expr); err == nil {
			t.Errorf("ParseSchedule(%q) expected error, got nil", expr)
		}
	}
}

func TestEverySchedule(t *testing.T) {
	s := mustParse(t, "@every 15m")
	base := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	next := s.Next(base)
	if want := base.Add(15 * time.Minute); !next.Equal(want) {
		t.Errorf("Next = %v, want %v", next, want)
	}
	if es, ok := s.(everySchedule); !ok || es.Interval() != 15*time.Minute {
		t.Errorf("expected everySchedule with 15m interval")
	}
}

func TestCronDaily(t *testing.T) {
	s := mustParse(t, "0 2 * * *")
	base := time.Date(2025, 1, 15, 1, 0, 0, 0, time.UTC)
	next := s.Next(base)
	want := time.Date(2025, 1, 15, 2, 0, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Errorf("Next = %v, want %v", next, want)
	}
	// After the fire time, it should roll to the next day.
	next2 := s.Next(want)
	want2 := time.Date(2025, 1, 16, 2, 0, 0, 0, time.UTC)
	if !next2.Equal(want2) {
		t.Errorf("Next = %v, want %v", next2, want2)
	}
}

func TestCronMacros(t *testing.T) {
	s := mustParse(t, "@hourly")
	base := time.Date(2025, 1, 15, 1, 30, 0, 0, time.UTC)
	next := s.Next(base)
	want := time.Date(2025, 1, 15, 2, 0, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Errorf("@hourly Next = %v, want %v", next, want)
	}
}

func TestCronWeekly(t *testing.T) {
	// Sunday 03:00.
	s := mustParse(t, "0 3 * * 0")
	// 2025-01-15 is a Wednesday; next Sunday is 2025-01-19.
	base := time.Date(2025, 1, 15, 4, 0, 0, 0, time.UTC)
	next := s.Next(base)
	want := time.Date(2025, 1, 19, 3, 0, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Errorf("weekly Next = %v, want %v", next, want)
	}
}

func TestCronDowSevenIsSunday(t *testing.T) {
	s7 := mustParse(t, "0 3 * * 7")
	s0 := mustParse(t, "0 3 * * 0")
	base := time.Date(2025, 1, 15, 4, 0, 0, 0, time.UTC)
	if !s7.Next(base).Equal(s0.Next(base)) {
		t.Errorf("dow=7 should equal dow=0 (Sunday)")
	}
}

func TestCronStepAndRange(t *testing.T) {
	// Every 15 minutes via step.
	s := mustParse(t, "*/15 * * * *")
	base := time.Date(2025, 1, 15, 10, 1, 0, 0, time.UTC)
	next := s.Next(base)
	want := time.Date(2025, 1, 15, 10, 15, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Errorf("step Next = %v, want %v", next, want)
	}
}

func TestCronDomDowOr(t *testing.T) {
	// Cron OR semantics: fires on the 1st OR on Mondays.
	s := mustParse(t, "0 0 1 * 1")
	// 2025-01-06 is a Monday.
	base := time.Date(2025, 1, 3, 12, 0, 0, 0, time.UTC)
	next := s.Next(base)
	want := time.Date(2025, 1, 6, 0, 0, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Errorf("dom/dow OR Next = %v, want %v", next, want)
	}
}
