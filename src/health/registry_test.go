package health

import (
	"sync/atomic"
	"testing"
)

func TestRegisterValidatesAndPreservesOrder(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register("Invalid-Key", false, func() string { return "ok" }); err == nil {
		t.Fatal("Register accepted a non-snake-case key")
	}
	if err := registry.Register("data_nil", false, nil); err == nil {
		t.Fatal("Register accepted a nil probe")
	}
	if err := registry.Register("data_first", true, func() string { return "ok" }); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register("data_second", false, func() string { return "degraded" }); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register("data_first", false, func() string { return "error" }); err == nil {
		t.Fatal("Register accepted a duplicate key")
	}

	registry.RefreshNow()
	got := registry.Snapshot()
	if len(got) != 2 || got[0] != (Result{Key: "data_first", State: "ok", Critical: true}) || got[1] != (Result{Key: "data_second", State: "degraded"}) {
		t.Fatalf("Snapshot() = %#v, want ordered safe results", got)
	}
}

func TestRefreshCachesResultsUntilForced(t *testing.T) {
	registry := NewRegistry()
	var calls atomic.Int32
	if err := registry.Register("data_change", false, func() string {
		if calls.Add(1) == 1 {
			return "ok"
		}
		return "degraded"
	}); err != nil {
		t.Fatal(err)
	}

	registry.Refresh()
	registry.Refresh()
	if got := registry.Snapshot()[0].State; got != "ok" || calls.Load() != 1 {
		t.Fatalf("cached refresh state=%q calls=%d, want ok and one probe", got, calls.Load())
	}
	registry.RefreshNow()
	if got := registry.Snapshot()[0].State; got != "degraded" || calls.Load() != 2 {
		t.Fatalf("forced refresh state=%q calls=%d, want degraded and two probes", got, calls.Load())
	}
}

func TestRefreshNormalizesInvalidAndPanickingProbes(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register("data_invalid", false, func() string { return "stale" }); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register("data_panic", false, func() string { panic("private probe details") }); err != nil {
		t.Fatal(err)
	}
	registry.RefreshNow()
	for _, result := range registry.Snapshot() {
		if result.State != "error" {
			t.Errorf("%s state = %q, want error", result.Key, result.State)
		}
	}
}
