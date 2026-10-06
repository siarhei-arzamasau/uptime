package store

import (
	"testing"
	"time"
)

func TestNextCheck(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name    string
		elapsed time.Duration
		seconds int
		want    time.Duration
	}{
		{"fast", time.Millisecond, 5, 5 * time.Second},
		{"exact slot", 5 * time.Second, 5, 10 * time.Second},
		{"slow", 12 * time.Second, 5, 15 * time.Second},
		{"restart", time.Hour, 5, time.Hour + 5*time.Second},
		{"maximum interval", time.Second, 2147483647, 2147483647 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := NextCheck(start, start.Add(tc.elapsed), tc.seconds)
			if !got.Equal(start.Add(tc.want)) {
				t.Fatalf("got %v want %v", got, start.Add(tc.want))
			}
		})
	}
}
