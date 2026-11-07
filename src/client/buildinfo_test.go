package main

import (
	"testing"
	"time"
)

func TestBuildDateIsRFC3339UTC(t *testing.T) {
	got := buildDate(1765112713)
	parsed, err := time.Parse(time.RFC3339, got)
	if err != nil {
		t.Fatalf("buildDate() = %q, not RFC 3339: %v", got, err)
	}
	if parsed.Location() != time.UTC {
		t.Errorf("buildDate() location = %v, want UTC", parsed.Location())
	}
}
