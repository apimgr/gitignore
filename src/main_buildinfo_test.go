package main

import (
	"testing"
	"time"
)

func TestBuildEpochAndDate(t *testing.T) {
	original := BuildEpoch
	t.Cleanup(func() { BuildEpoch = original })

	for _, test := range []struct {
		value string
		want  int64
	}{
		{value: "1765112713", want: 1765112713},
		{value: "0", want: 0},
		{value: "invalid", want: 0},
	} {
		BuildEpoch = test.value
		if got := buildEpoch(); got != test.want {
			t.Errorf("buildEpoch() with %q = %d, want %d", test.value, got, test.want)
		}
	}

	got := buildDate(1765112713)
	if _, err := time.Parse(time.RFC3339, got); err != nil {
		t.Fatalf("buildDate() = %q, not RFC 3339: %v", got, err)
	}
}
