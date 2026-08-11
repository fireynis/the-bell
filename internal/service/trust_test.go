package service

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
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
		name  string
		count int
		avgT  float64
		want  float64
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
				{Points: 10, CreatedAt: now, DecayDays: 90},                     // full: 10
				{Points: 10, CreatedAt: now.AddDate(0, 0, -45), DecayDays: 90},  // half: 5
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
		name            string
		joinedAt        time.Time
		posts           int
		reactions       int
		vouches         int
		avgVoucheeTrust float64
		penalties       []ActivePenalty
		wantComposite   float64
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

// --- toActivePenalty ---

func TestToActivePenalty(t *testing.T) {
	created := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	decaysAt := func(d time.Time) *time.Time { return &d }

	tests := []struct {
		name          string
		penalty       domain.TrustPenalty
		wantDecayDays int
	}{
		{
			name:          "nil DecaysAt is permanent",
			penalty:       domain.TrustPenalty{PenaltyAmount: 100, CreatedAt: created},
			wantDecayDays: 0,
		},
		{
			name:          "90 day window",
			penalty:       domain.TrustPenalty{PenaltyAmount: 25, CreatedAt: created, DecaysAt: decaysAt(created.AddDate(0, 0, 90))},
			wantDecayDays: 90,
		},
		{
			name:          "365 day window",
			penalty:       domain.TrustPenalty{PenaltyAmount: 40, CreatedAt: created, DecaysAt: decaysAt(created.AddDate(0, 0, 365))},
			wantDecayDays: 365,
		},
		{
			name:          "a window shorter than a day is clamped to one, never to permanent",
			penalty:       domain.TrustPenalty{PenaltyAmount: 5, CreatedAt: created, DecaysAt: decaysAt(created.Add(6 * time.Hour))},
			wantDecayDays: 1,
		},
		{
			name:          "a DecaysAt before CreatedAt cannot become permanent either",
			penalty:       domain.TrustPenalty{PenaltyAmount: 5, CreatedAt: created, DecaysAt: decaysAt(created.AddDate(0, 0, -10))},
			wantDecayDays: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toActivePenalty(tt.penalty)
			if got.DecayDays != tt.wantDecayDays {
				t.Errorf("DecayDays = %d, want %d", got.DecayDays, tt.wantDecayDays)
			}
			if got.Points != tt.penalty.PenaltyAmount {
				t.Errorf("Points = %v, want %v", got.Points, tt.penalty.PenaltyAmount)
			}
			if !got.CreatedAt.Equal(tt.penalty.CreatedAt) {
				t.Errorf("CreatedAt = %v, want %v", got.CreatedAt, tt.penalty.CreatedAt)
			}
		})
	}
}

// DecaysAt is written as CreatedAt.AddDate(0,0,n), which adds CALENDAR days. In
// a zone with daylight saving the wall-clock delta is 89.958 or 90.042 days, so
// truncating instead of rounding would silently record an 89-day window and
// decay every penalty a day early.
func TestToActivePenalty_RoundsAcrossDSTTransitions(t *testing.T) {
	nyc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}

	tests := []struct {
		name    string
		created time.Time
		days    int
	}{
		// Spring forward on 2026-03-08: the window loses an hour.
		{"window spanning spring forward", time.Date(2026, 2, 1, 12, 0, 0, 0, nyc), 90},
		// Fall back on 2026-11-01: the window gains an hour.
		{"window spanning fall back", time.Date(2026, 10, 1, 12, 0, 0, 0, nyc), 90},
		{"full year spanning both", time.Date(2026, 1, 15, 12, 0, 0, 0, nyc), 365},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decaysAt := tt.created.AddDate(0, 0, tt.days)
			// Confirm the trap is real: the raw delta is not a whole number of days.
			rawDays := decaysAt.Sub(tt.created).Hours() / 24
			if rawDays == float64(tt.days) {
				t.Logf("no DST shift in this window (raw = %v)", rawDays)
			}
			if truncated := int(rawDays); truncated == tt.days-1 {
				t.Logf("truncation would have yielded %d instead of %d", truncated, tt.days)
			}

			got := toActivePenalty(domain.TrustPenalty{
				PenaltyAmount: 25, CreatedAt: tt.created, DecaysAt: &decaysAt,
			})
			if got.DecayDays != tt.days {
				t.Errorf("DecayDays = %d, want %d (raw delta %v days)", got.DecayDays, tt.days, rawDays)
			}
		})
	}
}

// CalcModerationScore reads DecayDays 0 as permanent. Confirm that directly
// rather than assuming it, because toActivePenalty relies on it to represent a
// nil DecaysAt with no special case of its own.
func TestToActivePenalty_PermanentPenaltyNeverDecays(t *testing.T) {
	created := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	ban := toActivePenalty(domain.TrustPenalty{PenaltyAmount: 100, CreatedAt: created})

	if ban.DecayDays != 0 {
		t.Fatalf("DecayDays = %d, want 0 for a permanent penalty", ban.DecayDays)
	}

	// Six years on, the ban still zeroes the moderation component.
	sixYearsLater := created.AddDate(6, 0, 0)
	if got := CalcModerationScore([]ActivePenalty{ban}, sixYearsLater); got != 0 {
		t.Errorf("CalcModerationScore() = %v, want 0 — a permanent penalty must not decay", got)
	}
}

// --- CalcCompositeTrust ---

// fakeTrustInputs is an in-process TrustInputs. It is a collaborator, not an
// external resource: the point of the tests below is the arithmetic and the
// window CalcCompositeTrust derives, not the queries themselves.
type fakeTrustInputs struct {
	user      *domain.User
	posts     int64
	reactions int64
	vouches   int64
	avgTrust  float64
	penalties []domain.TrustPenalty

	// sinceSeen records the window boundary the calculator asked for.
	sinceSeen time.Time

	userErr      error
	postsErr     error
	reactionsErr error
	vouchesErr   error
	penaltiesErr error
}

func (f *fakeTrustInputs) GetUserByID(_ context.Context, _ string) (*domain.User, error) {
	if f.userErr != nil {
		return nil, f.userErr
	}
	return f.user, nil
}

func (f *fakeTrustInputs) CountPostsByAuthorSince(_ context.Context, _ string, since time.Time) (int64, error) {
	f.sinceSeen = since
	return f.posts, f.postsErr
}

func (f *fakeTrustInputs) CountReactionsReceivedByAuthorSince(_ context.Context, _ string, _ time.Time) (int64, error) {
	return f.reactions, f.reactionsErr
}

func (f *fakeTrustInputs) CountActiveVouchesWithAvgTrust(_ context.Context, _ string) (int64, float64, error) {
	return f.vouches, f.avgTrust, f.vouchesErr
}

func (f *fakeTrustInputs) ListActivePenaltiesByUser(_ context.Context, _ string) ([]domain.TrustPenalty, error) {
	return f.penalties, f.penaltiesErr
}

func TestCalcCompositeTrust(t *testing.T) {
	now := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	decaysAt := func(d time.Time) *time.Time { return &d }

	tests := []struct {
		name   string
		inputs *fakeTrustInputs
		want   float64
	}{
		{
			// Nothing but a join date. Tenure 0, activity 0, no vouches, no
			// penalties: a brand-new account scores 30, entirely from the
			// unblemished moderation component.
			name: "brand-new user scores only the clean moderation component",
			inputs: &fakeTrustInputs{
				user: &domain.User{ID: "new", JoinedAt: now},
			},
			want: 30.0,
		},
		{
			// tenure 100 (2 years), activity 100 (both caps), voucher 100
			// (7 vouches from perfect-trust neighbours), moderation 100.
			name: "fully established user reaches the ceiling",
			inputs: &fakeTrustInputs{
				user:      &domain.User{ID: "veteran", JoinedAt: now.AddDate(-2, 0, 0)},
				posts:     90,
				reactions: 270,
				vouches:   7,
				avgTrust:  100,
			},
			want: 100.0,
		},
		{
			// tenure = (365/365)*100 = 100 -> 15
			// activity = (45/90*100)*0.5 + (135/270*100)*0.5 = 50 -> 10
			// voucher = min(100, 3*15) * (80/100) = 45*0.8 = 36 -> 12.6
			// moderation = 100 -> 30
			name: "long-tenured active user",
			inputs: &fakeTrustInputs{
				user:      &domain.User{ID: "active", JoinedAt: now.AddDate(0, 0, -365)},
				posts:     45,
				reactions: 135,
				vouches:   3,
				avgTrust:  80,
			},
			want: 67.6,
		},
		{
			// A permanent penalty (nil DecaysAt) wipes the moderation
			// component and never decays, even years later. The user here is
			// deliberately NOT banned — a ban floors the whole score, see
			// TestCalcCompositeTrust_BannedUserScoresZero. This is the other
			// holder of a permanent penalty: someone who vouched for a banned
			// user, since propagated penalties inherit the nil DecaysAt.
			name: "permanent penalty costs a voucher the whole moderation component",
			inputs: &fakeTrustInputs{
				user:      &domain.User{ID: "voucher", JoinedAt: now.AddDate(-2, 0, 0), Role: domain.RoleMember},
				posts:     90,
				reactions: 270,
				vouches:   7,
				avgTrust:  100,
				penalties: []domain.TrustPenalty{
					{PenaltyAmount: 100, CreatedAt: now.AddDate(-1, 0, 0)},
				},
			},
			// 100*0.15 + 100*0.20 + 100*0.35 - moderation contributes 0
			want: 70.0,
		},
		{
			// A severity-3 penalty (25 points, 270 day window) half elapsed:
			// 25 * (1 - 135/270) = 12.5 remaining, moderation = 87.5 -> 26.25.
			// tenure 100 -> 15, activity 0, voucher 0.
			name: "half-decayed penalty returns half its bite",
			inputs: &fakeTrustInputs{
				user: &domain.User{ID: "recovering", JoinedAt: now.AddDate(0, 0, -365)},
				penalties: []domain.TrustPenalty{
					{
						PenaltyAmount: 25,
						CreatedAt:     now.AddDate(0, 0, -135),
						DecaysAt:      decaysAt(now.AddDate(0, 0, -135).AddDate(0, 0, 270)),
					},
				},
			},
			want: 41.25,
		},
		{
			// Penalties far exceeding 100 cannot drive the moderation
			// component, or the composite, below zero.
			name: "stacked penalties clamp at the floor, not below",
			inputs: &fakeTrustInputs{
				user: &domain.User{ID: "floored", JoinedAt: now},
				penalties: []domain.TrustPenalty{
					{PenaltyAmount: 100, CreatedAt: now},
					{PenaltyAmount: 100, CreatedAt: now},
					{PenaltyAmount: 100, CreatedAt: now},
				},
			},
			want: 0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := CalcCompositeTrust(context.Background(), tt.inputs, tt.inputs.user.ID, now)
			if err != nil {
				t.Fatalf("CalcCompositeTrust() unexpected error: %v", err)
			}
			if !approxEqual(got, tt.want) {
				t.Errorf("CalcCompositeTrust() = %v, want %v", got, tt.want)
			}
			if got < 0 || got > 100 {
				t.Errorf("CalcCompositeTrust() = %v, outside the 0-100 range", got)
			}
		})
	}
}

// The activity component is defined over a 90-day rolling window, and the
// calculator is the only thing that knows that — the repositories just take a
// cutoff.
func TestCalcCompositeTrust_AsksForA90DayWindow(t *testing.T) {
	now := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	inputs := &fakeTrustInputs{user: &domain.User{ID: "u", JoinedAt: now}}

	if _, err := CalcCompositeTrust(context.Background(), inputs, "u", now); err != nil {
		t.Fatalf("CalcCompositeTrust() unexpected error: %v", err)
	}

	want := now.AddDate(0, 0, -activityWindowDays)
	if !inputs.sinceSeen.Equal(want) {
		t.Errorf("window start = %v, want %v", inputs.sinceSeen, want)
	}
}

// A trust score written from partial data would be wrong in a way nothing
// downstream could detect, so any failed lookup aborts the whole calculation.
func TestCalcCompositeTrust_AnyLookupFailureAborts(t *testing.T) {
	now := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	dbDown := errors.New("db connection lost")

	tests := []struct {
		name  string
		setup func(*fakeTrustInputs)
	}{
		{"user lookup fails", func(f *fakeTrustInputs) { f.userErr = dbDown }},
		{"post count fails", func(f *fakeTrustInputs) { f.postsErr = dbDown }},
		{"reaction count fails", func(f *fakeTrustInputs) { f.reactionsErr = dbDown }},
		{"vouch count fails", func(f *fakeTrustInputs) { f.vouchesErr = dbDown }},
		{"penalty lookup fails", func(f *fakeTrustInputs) { f.penaltiesErr = dbDown }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inputs := &fakeTrustInputs{user: &domain.User{ID: "u", JoinedAt: now}}
			tt.setup(inputs)

			score, err := CalcCompositeTrust(context.Background(), inputs, "u", now)
			if !errors.Is(err, dbDown) {
				t.Fatalf("error = %v, want it to wrap %v", err, dbDown)
			}
			if score != 0 {
				t.Errorf("score = %v, want 0 alongside the error", score)
			}
		})
	}
}

// --- The trust score is not the mute mechanism ---

// establishedMember is a fully established member: two years' tenure, capped
// activity, seven vouches from perfect-trust neighbours. Their three
// non-moderation components total 70 on their own.
func establishedMember(now time.Time, penalties []domain.TrustPenalty) *fakeTrustInputs {
	return &fakeTrustInputs{
		user:      &domain.User{ID: "member", JoinedAt: now.AddDate(-2, 0, 0)},
		posts:     90,
		reactions: 270,
		vouches:   7,
		avgTrust:  100,
		penalties: penalties,
	}
}

// Penalties alone can never enforce a mute, whatever their size. They reduce
// only the moderation component, which carries 30% of the weight, so an
// established member keeps up to 70 points from tenure, activity and vouches —
// well clear of the posting threshold of 30.
//
// This is the reasoning that justified domain.User.MutedUntil. Silencing
// someone by driving their trust score down cannot work for exactly the members
// most likely to be moderated, so a mute has to be its own piece of state
// rather than a number the trust model is free to recompute. Keep this test:
// if it ever starts failing, the weighting has shifted enough that somebody
// will be tempted to reach for the score again.
func TestCalcCompositeTrust_PenaltiesAloneCannotSilenceAnEstablishedMember(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	// A penalty far larger than any severity can produce.
	inputs := establishedMember(now, []domain.TrustPenalty{
		{ID: "huge", UserID: "member", PenaltyAmount: 1000, CreatedAt: now},
	})

	score, err := CalcCompositeTrust(context.Background(), inputs, "member", now)
	if err != nil {
		t.Fatalf("CalcCompositeTrust() unexpected error: %v", err)
	}
	if score != 70 {
		t.Errorf("score = %v, want 70 — the three non-moderation components at full marks", score)
	}
	if score < domain.PostingThreshold {
		t.Fatalf("score = %v: a penalty alone silenced the user, which would undercut "+
			"the reason MutedUntil exists", score)
	}
}

// The composite must not special-case mutes. Muting is enforced by
// domain.User.MutedUntil, which CanPost consults independently of the score, so
// the calculation stays a pure function of the four components and a muted user
// scores exactly what an unmuted one with the same history would.
//
// An earlier interim fix clamped the score for muted users, which meant two
// mute mechanisms were live at once. This pins the separation so that clamp
// cannot creep back in.
func TestCalcCompositeTrust_DoesNotSpecialCaseMutedUsers(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	// The severity-3 penalty a mute writes: 25 points over 270 days.
	decaysAt := now.AddDate(0, 0, 270)
	penalties := []domain.TrustPenalty{
		{ID: "penalty-mute", UserID: "member", PenaltyAmount: 25, CreatedAt: now, DecaysAt: &decaysAt},
	}

	unmuted := establishedMember(now, penalties)
	muted := establishedMember(now, penalties)
	mutedUntil := now.Add(time.Hour)
	muted.user.MutedUntil = &mutedUntil

	unmutedScore, err := CalcCompositeTrust(context.Background(), unmuted, "member", now)
	if err != nil {
		t.Fatalf("CalcCompositeTrust() unexpected error: %v", err)
	}
	mutedScore, err := CalcCompositeTrust(context.Background(), muted, "member", now)
	if err != nil {
		t.Fatalf("CalcCompositeTrust() unexpected error: %v", err)
	}

	if mutedScore != unmutedScore {
		t.Errorf("muted score = %v, unmuted score = %v — the composite is special-casing mutes",
			mutedScore, unmutedScore)
	}
	// 70 from the other components, plus (100-25)*0.30 = 22.5.
	if want := 92.5; mutedScore != want {
		t.Errorf("score = %v, want %v — the four components and nothing else", mutedScore, want)
	}

	// The score alone leaves them able to post; MutedUntil is what stops them.
	muted.user.TrustScore = mutedScore
	muted.user.Role = domain.RoleMember
	muted.user.IsActive = true
	if !muted.user.IsMuted(now) {
		t.Fatal("test setup: expected the user to be muted")
	}
	if muted.user.CanPost(now) {
		t.Error("CanPost() = true for a muted user")
	}
	if muted.user.TrustScore < domain.PostingThreshold {
		t.Errorf("trust = %v: the score is below the posting threshold, so this test would "+
			"pass even if MutedUntil were broken", muted.user.TrustScore)
	}
}

// A ban is the one moderation outcome the four components cannot express. The
// permanent 100-point penalty only empties the moderation component, which
// carries 30% of the weight, so an established member recomputes to 70 — the
// exact score TakeAction had just written to zero. The role is the
// authoritative record of a ban, so the calculation reads that rather than
// trying to infer a ban from the shape of the penalties.
//
// The displayed score is the visible half of this; the damaging half is that
// CountActiveVouchesWithAvgTrust averages a voucher's stored trust into their
// vouchees' voucher component, so a banned account restored to 70 would keep
// propping up everyone it ever vouched for.
func TestCalcCompositeTrust_BannedUserScoresZero(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

	// Everything a ban writes: the banned role, plus the permanent severity-5
	// penalty PropagatePenalties records for it.
	inputs := establishedMember(now, []domain.TrustPenalty{
		{ID: "penalty-ban", UserID: "member", PenaltyAmount: 100, CreatedAt: now},
	})
	inputs.user.Role = domain.RoleBanned

	score, err := CalcCompositeTrust(context.Background(), inputs, "member", now)
	if err != nil {
		t.Fatalf("CalcCompositeTrust() unexpected error: %v", err)
	}
	if score != 0 {
		t.Errorf("score = %v, want 0 — a recalculation handed a banned user back "+
			"the 70 points TakeAction had just zeroed", score)
	}
}

// The floor keys on the role, not on the penalty, because the two are not the
// same population: propagated penalties from a ban carry the same nil DecaysAt
// as the direct one, so a voucher two hops from a banned user holds a permanent
// penalty without being banned themselves. They keep their other components.
func TestCalcCompositeTrust_PermanentPenaltyAloneIsNotABan(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

	inputs := establishedMember(now, []domain.TrustPenalty{
		{ID: "penalty-propagated", UserID: "member", PenaltyAmount: 75, CreatedAt: now},
	})
	inputs.user.Role = domain.RoleMember

	score, err := CalcCompositeTrust(context.Background(), inputs, "member", now)
	if err != nil {
		t.Fatalf("CalcCompositeTrust() unexpected error: %v", err)
	}
	// 70 from the other components, plus (100-75)*0.30 = 7.5.
	if want := 77.5; !approxEqual(score, want) {
		t.Errorf("score = %v, want %v — a voucher of a banned user is not banned", score, want)
	}
}

// Suspend and ban are enforced through fields the recalculation never touches,
// so a restored score cannot revive either one.
func TestSuspendAndBanSurviveARestoredScore(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	const restored = 100.0

	tests := []struct {
		name string
		user *domain.User
	}{
		{
			"a suspended user stays unable to post: DeactivateUser cleared IsActive",
			&domain.User{Role: domain.RoleMember, IsActive: false, TrustScore: restored},
		},
		{
			"a banned user stays unable to post: the role is what blocks them",
			&domain.User{Role: domain.RoleBanned, IsActive: true, TrustScore: restored},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.user.CanPost(now) {
				t.Error("CanPost() = true after a full-score recalculation")
			}
			if tt.user.CanVouch() {
				t.Error("CanVouch() = true after a full-score recalculation")
			}
		})
	}
}
