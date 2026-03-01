package service

import (
	"math"
	"testing"
	"time"
)

const epsilon = 0.01

func approxEqual(a, b float64) bool {
	return math.Abs(a-b) < epsilon
}

func TestCalcTenureScore(t *testing.T) {
	now := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		joinedAt time.Time
		want     float64
	}{
		{"brand new user", now, 0},
		{"half year", now.AddDate(0, 0, -183), 50.14},
		{"exactly 365 days", now.AddDate(0, 0, -365), 100.0},
		{"two years", now.AddDate(-2, 0, 0), 100.0},
		{"future join date", now.Add(24 * time.Hour), 0},
		{"one day", now.AddDate(0, 0, -1), 0.27},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CalcTenureScore(tt.joinedAt, now)
			if !approxEqual(got, tt.want) {
				t.Errorf("CalcTenureScore() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCalcActivityScore(t *testing.T) {
	tests := []struct {
		name      string
		posts     int
		reactions int
		want      float64
	}{
		{"zero activity", 0, 0, 0},
		{"perfect activity", 90, 270, 100.0},
		{"only posts at cap", 90, 0, 50.0},
		{"only reactions at cap", 0, 270, 50.0},
		{"half posts half reactions", 45, 135, 50.0},
		{"over cap", 200, 500, 100.0},
		{"negative posts clamped", -10, 135, 25.0},
		{"negative reactions clamped", 45, -10, 25.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CalcActivityScore(tt.posts, tt.reactions)
			if !approxEqual(got, tt.want) {
				t.Errorf("CalcActivityScore() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCalcVoucherScore(t *testing.T) {
	tests := []struct {
		name   string
		count  int
		avgT   float64
		want   float64
	}{
		{"zero vouches", 0, 100, 0},
		{"negative vouches", -1, 100, 0},
		{"7 vouches perfect trust", 7, 100, 100.0},
		{"3 vouches 80 trust", 3, 80, 36.0},
		{"10 vouches 50 trust", 10, 50, 50.0},
		{"vouches with zero trust", 5, 0, 0},
		{"trust over 100 clamped", 7, 150, 100.0},
		{"negative trust clamped", 3, -20, 0},
		{"1 vouch 100 trust", 1, 100, 15.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CalcVoucherScore(tt.count, tt.avgT)
			if !approxEqual(got, tt.want) {
				t.Errorf("CalcVoucherScore() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCalcModerationScore(t *testing.T) {
	now := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name      string
		penalties []ActivePenalty
		want      float64
	}{
		{"no penalties", nil, 100.0},
		{"empty slice", []ActivePenalty{}, 100.0},
		{
			"fresh 5-point penalty",
			[]ActivePenalty{{Points: 5, CreatedAt: now, DecayDays: 90}},
			95.0,
		},
		{
			"fully decayed penalty",
			[]ActivePenalty{{Points: 5, CreatedAt: now.AddDate(0, 0, -100), DecayDays: 90}},
			100.0,
		},
		{
			"permanent penalty",
			[]ActivePenalty{{Points: 100, CreatedAt: now.AddDate(-1, 0, 0), DecayDays: 0}},
			0,
		},
		{
			"half decayed penalty",
			[]ActivePenalty{{Points: 10, CreatedAt: now.AddDate(0, 0, -90), DecayDays: 180}},
			95.0,
		},
		{
			"penalties exceeding 100 clamped to 0",
			[]ActivePenalty{
				{Points: 60, CreatedAt: now, DecayDays: 365},
				{Points: 60, CreatedAt: now, DecayDays: 365},
			},
			0,
		},
		{
			"future penalty applies full points",
			[]ActivePenalty{{Points: 20, CreatedAt: now.Add(24 * time.Hour), DecayDays: 90}},
			80.0,
		},
		{
			"mix of active partially-decayed and fully-decayed",
			[]ActivePenalty{
				{Points: 10, CreatedAt: now, DecayDays: 90},             // full: 10
				{Points: 10, CreatedAt: now.AddDate(0, 0, -45), DecayDays: 90}, // half: 5
				{Points: 10, CreatedAt: now.AddDate(0, 0, -100), DecayDays: 90}, // decayed: 0
			},
			85.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CalcModerationScore(tt.penalties, now)
			if !approxEqual(got, tt.want) {
				t.Errorf("CalcModerationScore() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCompositeScore(t *testing.T) {
	tests := []struct {
		name       string
		tenure     float64
		activity   float64
		voucher    float64
		moderation float64
		want       float64
	}{
		{"all zeros", 0, 0, 0, 0, 0},
		{"all 100s", 100, 100, 100, 100, 100.0},
		{"only tenure", 100, 0, 0, 0, 15.0},
		{"only activity", 0, 100, 0, 0, 20.0},
		{"only voucher", 0, 0, 100, 0, 35.0},
		{"only moderation", 0, 0, 0, 100, 30.0},
		{
			"realistic scenario",
			50, 60, 70, 90,
			// 50*0.15 + 60*0.20 + 70*0.35 + 90*0.30 = 7.5 + 12 + 24.5 + 27 = 71
			71.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CompositeScore(tt.tenure, tt.activity, tt.voucher, tt.moderation)
			if !approxEqual(got, tt.want) {
				t.Errorf("CompositeScore() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEndToEndScenarios(t *testing.T) {
	now := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name             string
		joinedAt         time.Time
		posts            int
		reactions        int
		vouches          int
		avgVoucheeTrust  float64
		penalties        []ActivePenalty
		wantComposite    float64
	}{
		{
			name:            "brand new user, no activity",
			joinedAt:        now,
			posts:           0,
			reactions:       0,
			vouches:         0,
			avgVoucheeTrust: 0,
			penalties:       nil,
			// tenure=0, activity=0, voucher=0, moderation=100
			// 0*0.15 + 0*0.20 + 0*0.35 + 100*0.30 = 30
			wantComposite: 30.0,
		},
		{
			name:            "established member",
			joinedAt:        now.AddDate(-1, 0, 0),
			posts:           60,
			reactions:       200,
			vouches:         5,
			avgVoucheeTrust: 80,
			penalties:       nil,
			// tenure=100, activity=(66.67*0.5)+(74.07*0.5)=33.33+37.04=70.37
			// voucher=min(100,75)*0.8=60.0, moderation=100
			// 100*0.15 + 70.37*0.20 + 60*0.35 + 100*0.30
			// = 15 + 14.07 + 21 + 30 = 80.07
			wantComposite: 80.07,
		},
		{
			name:            "penalized user",
			joinedAt:        now.AddDate(0, -6, 0),
			posts:           30,
			reactions:       90,
			vouches:         2,
			avgVoucheeTrust: 70,
			penalties: []ActivePenalty{
				{Points: 25, CreatedAt: now.AddDate(0, 0, -30), DecayDays: 270},
			},
			// tenure ~ (182/365)*100 ≈ 49.86
			// activity = (33.33*0.5)+(33.33*0.5) = 33.33
			// voucher = 30 * 0.7 = 21.0
			// penalty remaining: 25 * (1 - 30/270) ≈ 25 * 0.8889 ≈ 22.22
			// moderation = 100 - 22.22 = 77.78
			// tenure ≈ 49.59, activity = 33.33, voucher = 21, moderation ≈ 77.78
			// composite ≈ 49.59*0.15 + 33.33*0.20 + 21*0.35 + 77.78*0.30 ≈ 44.79
			wantComposite: 44.79,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tenure := CalcTenureScore(tt.joinedAt, now)
			activity := CalcActivityScore(tt.posts, tt.reactions)
			voucher := CalcVoucherScore(tt.vouches, tt.avgVoucheeTrust)
			moderation := CalcModerationScore(tt.penalties, now)
			got := CompositeScore(tenure, activity, voucher, moderation)

			if !approxEqual(got, tt.wantComposite) {
				t.Errorf("Composite = %v, want %v (tenure=%v activity=%v voucher=%v moderation=%v)",
					got, tt.wantComposite, tenure, activity, voucher, moderation)
			}
		})
	}
}
