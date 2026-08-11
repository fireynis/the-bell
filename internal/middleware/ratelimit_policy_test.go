package middleware

import (
	"testing"
	"time"
)

// formatWindow's output is part of the Redis key, so these labels are storage
// keys and not display strings. Changing one silently moves every user to a
// fresh bucket — everybody who was at their limit is suddenly at zero, and
// nothing fails or logs. The table pins the label for every window the routes
// actually use plus each branch's boundary, so a "tidier" format has to be an
// intentional edit to this file.
//
// The order of the branches matters too: a day is also a whole number of hours
// and of minutes, so 24h must land on "1d" and not "24h".
func TestFormatWindow(t *testing.T) {
	tests := []struct {
		name   string
		window time.Duration
		want   string
	}{
		// The three windows in server.apiRoutes today. If one of these labels
		// changes, that route's limit resets on deploy.
		{"posts: one hour", time.Hour, "1h"},
		{"reactions: one minute", time.Minute, "1m"},
		{"vouches: one day", 24 * time.Hour, "1d"},

		{"multiple days", 7 * 24 * time.Hour, "7d"},
		{"multiple hours", 12 * time.Hour, "12h"},
		{"multiple minutes", 15 * time.Minute, "15m"},

		// A whole number of days is also a whole number of hours and minutes;
		// the day branch has to win.
		{"48h is two days, not 48 hours", 48 * time.Hour, "2d"},
		// 90 minutes is not a whole number of hours, so it falls to minutes
		// rather than being truncated to "1h" — which would share a bucket
		// with an actual hourly limit.
		{"ninety minutes stays in minutes", 90 * time.Minute, "90m"},

		// Sub-minute windows fall through to seconds.
		{"thirty seconds", 30 * time.Second, "30s"},
		{"one second", time.Second, "1s"},
		{"ninety seconds is not a whole minute", 90 * time.Second, "90s"},
		// Truncation to whole seconds is the documented shape of the default
		// branch: sub-second windows are not a limit anyone configures, and
		// the label only has to be stable, not lossless.
		{"sub-second truncates", 1500 * time.Millisecond, "1s"},
		{"zero", 0, "0d"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatWindow(tt.window); got != tt.want {
				t.Errorf("formatWindow(%v) = %q, want %q", tt.window, got, tt.want)
			}
		})
	}
}

// Two different windows must never produce the same label: the label is the
// only part of the Redis key that distinguishes one limit from another for the
// same user and endpoint, so a collision merges two budgets into one.
func TestFormatWindow_DistinguishesTheConfiguredWindows(t *testing.T) {
	windows := []time.Duration{
		time.Minute, time.Hour, 24 * time.Hour,
		time.Second, 30 * time.Second, 15 * time.Minute, 12 * time.Hour, 7 * 24 * time.Hour,
	}

	seen := make(map[string]time.Duration, len(windows))
	for _, w := range windows {
		label := formatWindow(w)
		if prev, ok := seen[label]; ok {
			t.Errorf("formatWindow(%v) and formatWindow(%v) both return %q", prev, w, label)
		}
		seen[label] = w
	}
}
