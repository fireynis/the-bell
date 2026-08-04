package service

import (
	"testing"
	"time"
)

func TestStartOfDay(t *testing.T) {
	tests := []struct {
		name string
		now  time.Time
		want time.Time
	}{
		{
			name: "midday truncates to midnight",
			now:  time.Date(2026, 3, 1, 14, 30, 15, 500, time.UTC),
			want: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name: "midnight is already the start of its day",
			now:  time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
			want: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name: "the last minute of the day still belongs to that day",
			now:  time.Date(2026, 3, 1, 23, 59, 59, 0, time.UTC),
			want: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name: "the first minute of the next day starts a new window",
			now:  time.Date(2026, 3, 2, 0, 1, 0, 0, time.UTC),
			want: time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC),
		},
		{
			name: "a leap day is an ordinary day",
			now:  time.Date(2028, 2, 29, 9, 0, 0, 0, time.UTC),
			want: time.Date(2028, 2, 29, 0, 0, 0, 0, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := startOfDay(tt.now); !got.Equal(tt.want) {
				t.Errorf("startOfDay(%v) = %v, want %v", tt.now, got, tt.want)
			}
		})
	}
}

// Two vouches a couple of minutes apart across midnight must fall in different
// windows — this is the whole point of the daily limit resetting.
func TestStartOfDay_MidnightSeparatesWindows(t *testing.T) {
	lastMinute := time.Date(2026, 3, 1, 23, 59, 0, 0, time.UTC)
	firstMinute := time.Date(2026, 3, 2, 0, 1, 0, 0, time.UTC)

	if startOfDay(lastMinute).Equal(startOfDay(firstMinute)) {
		t.Fatalf("23:59 and 00:01 share a window starting at %v", startOfDay(lastMinute))
	}
	// A vouch made at 23:59 is outside the window that 00:01 counts over.
	if !lastMinute.Before(startOfDay(firstMinute)) {
		t.Error("a vouch at 23:59 would still be counted against the next day")
	}
}

// The window follows the clock's own location. A UTC-based cutoff would move a
// resident's reset to the middle of their afternoon or evening.
func TestStartOfDay_UsesTheClocksLocation(t *testing.T) {
	nyc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}

	// 01:00 in New York is already 06:00 UTC — the same instant, a different day
	// boundary.
	now := time.Date(2026, 3, 1, 1, 0, 0, 0, nyc)
	got := startOfDay(now)

	if got.Location() != nyc {
		t.Errorf("location = %v, want %v", got.Location(), nyc)
	}
	want := time.Date(2026, 3, 1, 0, 0, 0, 0, nyc)
	if !got.Equal(want) {
		t.Errorf("startOfDay(%v) = %v, want %v", now, got, want)
	}
	if utcMidnight := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC); got.Equal(utcMidnight) {
		t.Error("the window started at UTC midnight, not the town's own midnight")
	}
}

// On a DST transition the local day is 23 or 25 hours long. The window still
// runs from the day's own midnight, so a resident gets exactly one allowance
// per calendar day rather than one per 24 hours.
func TestStartOfDay_DSTTransitions(t *testing.T) {
	nyc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}

	tests := []struct {
		name          string
		now           time.Time
		wantSinceNoon time.Duration
	}{
		{
			// 2026-03-08: clocks jump 02:00 -> 03:00, so the morning is an hour short.
			name:          "spring forward loses an hour from the morning",
			now:           time.Date(2026, 3, 8, 12, 0, 0, 0, nyc),
			wantSinceNoon: 11 * time.Hour,
		},
		{
			// 2026-11-01: clocks fall 02:00 -> 01:00, so the morning gains an hour.
			name:          "fall back adds an hour to the morning",
			now:           time.Date(2026, 11, 1, 12, 0, 0, 0, nyc),
			wantSinceNoon: 13 * time.Hour,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := startOfDay(tt.now)

			if got.Day() != tt.now.Day() || got.Hour() != 0 {
				t.Errorf("startOfDay(%v) = %v, want local midnight on the same day", tt.now, got)
			}
			if elapsed := tt.now.Sub(got); elapsed != tt.wantSinceNoon {
				t.Errorf("noon is %v after the window opened, want %v", elapsed, tt.wantSinceNoon)
			}
			if got.After(tt.now) {
				t.Errorf("startOfDay(%v) = %v, which is in the future", tt.now, got)
			}
		})
	}
}
