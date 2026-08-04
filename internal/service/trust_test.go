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
	actions   []*domain.ModerationAction

	// sinceSeen records the window boundary the calculator asked for.
	sinceSeen time.Time

	userErr      error
	postsErr     error
	reactionsErr error
	vouchesErr   error
	penaltiesErr error
	actionsErr   error
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

func (f *fakeTrustInputs) ListActionsByTarget(_ context.Context, _ string, _, _ int) ([]*domain.ModerationAction, error) {
	return f.actions, f.actionsErr
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
			// A permanent ban penalty (nil DecaysAt) wipes the moderation
			// component and never decays, even years later.
			name: "permanently banned user loses the whole moderation component",
			inputs: &fakeTrustInputs{
				user:      &domain.User{ID: "banned", JoinedAt: now.AddDate(-2, 0, 0)},
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

// --- Mute survival ---

// mutedMember is a fully established member: two years' tenure, capped
// activity, seven vouches from perfect-trust neighbours. Their non-moderation
// components alone total 70, which is why no penalty can silence them — see
// TestCalcCompositeTrust_PenaltiesAloneCannotEnforceAMute.
func mutedMember(now time.Time, actions []*domain.ModerationAction, penalties []domain.TrustPenalty) *fakeTrustInputs {
	return &fakeTrustInputs{
		user:      &domain.User{ID: "muted", JoinedAt: now.AddDate(-2, 0, 0)},
		posts:     90,
		reactions: 270,
		vouches:   7,
		avgTrust:  100,
		penalties: penalties,
		actions:   actions,
	}
}

// muteAction builds a mute expiring at the given time. Every real mute has an
// expiry: validateActionRequest makes a duration mandatory for mutes.
func muteAction(created time.Time, expires *time.Time) *domain.ModerationAction {
	return &domain.ModerationAction{
		ID: "action-mute", TargetUserID: "muted", Action: domain.ActionMute,
		Severity: 3, Reason: "off-topic", CreatedAt: created, ExpiresAt: expires,
	}
}

// mutePenalty is the severity-3 penalty PropagatePenalties writes alongside a
// mute: 25 points decaying over 270 days.
func mutePenalty(created time.Time) domain.TrustPenalty {
	decaysAt := created.AddDate(0, 0, 270)
	return domain.TrustPenalty{
		ID: "penalty-mute", UserID: "muted", ModerationActionID: "action-mute",
		PenaltyAmount: 25, CreatedAt: created, DecaysAt: &decaysAt,
	}
}

// asUser wraps a computed score in the user record the permission checks read.
func asUser(score float64) *domain.User {
	return &domain.User{
		ID: "muted", Role: domain.RoleMember, IsActive: true, TrustScore: score,
	}
}

// This is the regression. A mute is enforced solely by writing the target's
// trust below the posting threshold — domain.User.CanPost consults nothing
// else, and planEnforcement emits only enforceDropBelowPostingThreshold. Once
// the recalculation queue went live, the recalc that a mute's own penalty
// triggers recomputed the composite and handed the score straight back,
// cancelling the mute within seconds of a moderator applying it.
func TestCalcCompositeTrust_ActiveMuteKeepsTheUserSilenced(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	expires := now.Add(time.Hour)
	inputs := mutedMember(now, []*domain.ModerationAction{muteAction(now, &expires)}, []domain.TrustPenalty{mutePenalty(now)})

	score, err := CalcCompositeTrust(context.Background(), inputs, "muted", now)
	if err != nil {
		t.Fatalf("CalcCompositeTrust() unexpected error: %v", err)
	}

	if user := asUser(score); user.CanPost() {
		t.Errorf("recalculated trust = %v, and CanPost() = true — the recalculation cancelled an active mute", score)
	}
	if score >= domain.PostingThreshold {
		t.Errorf("score = %v, want it held below the posting threshold %v while muted", score, domain.PostingThreshold)
	}
}

// The mute must end when the moderator said it ends. The penalty behind it
// decays over 270 days, so keying the clamp on the penalty rather than on the
// action's expiry would silently turn a one-hour mute into a nine-month one.
func TestCalcCompositeTrust_ExpiredMuteReleasesTheUser(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	// Muted an hour ago for one minute: long over, but its 270-day penalty is
	// still very much active.
	created := now.Add(-time.Hour)
	expires := created.Add(time.Minute)
	inputs := mutedMember(now, []*domain.ModerationAction{muteAction(created, &expires)}, []domain.TrustPenalty{mutePenalty(created)})

	score, err := CalcCompositeTrust(context.Background(), inputs, "muted", now)
	if err != nil {
		t.Fatalf("CalcCompositeTrust() unexpected error: %v", err)
	}

	if user := asUser(score); !user.CanPost() {
		t.Errorf("score = %v, and CanPost() = false — an expired mute is still silencing the user", score)
	}
}

// A warn carries a trust penalty but is not a mute, so it must not silence
// anyone: it only moves the moderation component.
func TestCalcCompositeTrust_WarnDoesNotSilence(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	decaysAt := now.AddDate(0, 0, 90)
	warn := &domain.ModerationAction{
		ID: "action-warn", TargetUserID: "muted", Action: domain.ActionWarn,
		Severity: 1, Reason: "reminder", CreatedAt: now,
	}
	penalty := domain.TrustPenalty{
		ID: "penalty-warn", UserID: "muted", ModerationActionID: "action-warn",
		PenaltyAmount: 5, CreatedAt: now, DecaysAt: &decaysAt,
	}
	inputs := mutedMember(now, []*domain.ModerationAction{warn}, []domain.TrustPenalty{penalty})

	score, err := CalcCompositeTrust(context.Background(), inputs, "muted", now)
	if err != nil {
		t.Fatalf("CalcCompositeTrust() unexpected error: %v", err)
	}
	if user := asUser(score); !user.CanPost() {
		t.Errorf("score = %v, and CanPost() = false — a warn must not silence anyone", score)
	}
}

// Penalties can never enforce a mute on their own, whatever their size. They
// only reduce the moderation component, which carries 30% of the weight, so an
// established member keeps up to 70 points from tenure, activity and vouches —
// well clear of the posting threshold. This is why the clamp has to exist
// rather than the penalty simply being made larger.
func TestCalcCompositeTrust_PenaltiesAloneCannotEnforceAMute(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	// No mute action, but a penalty far larger than any severity produces.
	inputs := mutedMember(now, nil, []domain.TrustPenalty{
		{ID: "huge", UserID: "muted", PenaltyAmount: 1000, CreatedAt: now},
	})

	score, err := CalcCompositeTrust(context.Background(), inputs, "muted", now)
	if err != nil {
		t.Fatalf("CalcCompositeTrust() unexpected error: %v", err)
	}
	if score < domain.PostingThreshold {
		t.Fatalf("score = %v: a penalty alone dropped the user below the threshold, "+
			"which would undermine the reason the clamp exists", score)
	}
	if score != 70 {
		t.Errorf("score = %v, want 70 — the three non-moderation components at full marks", score)
	}
}

func TestClampForActiveMute(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	future := now.Add(time.Hour)
	past := now.Add(-time.Hour)
	const capped = domain.PostingThreshold - 1

	tests := []struct {
		name    string
		score   float64
		actions []*domain.ModerationAction
		want    float64
	}{
		{"no moderation history leaves the score alone", 92.5, nil, 92.5},
		{
			"an unexpired mute caps the score below the posting threshold",
			92.5, []*domain.ModerationAction{muteAction(now, &future)}, capped,
		},
		{
			"an expired mute releases the score",
			92.5, []*domain.ModerationAction{muteAction(past, &past)}, 92.5,
		},
		{
			// A mute with no expiry cannot come from TakeAction. Treating it as
			// lapsed would let malformed data silently un-mute someone.
			"a mute with no expiry is treated as still running",
			92.5, []*domain.ModerationAction{muteAction(now, nil)}, capped,
		},
		{
			"a warn never caps",
			92.5,
			[]*domain.ModerationAction{{Action: domain.ActionWarn, CreatedAt: now}},
			92.5,
		},
		{
			// Suspend and ban are enforced through IsActive and Role, not the
			// score, so they must not disturb it here.
			"a suspension does not cap the score",
			92.5,
			[]*domain.ModerationAction{{Action: domain.ActionSuspend, CreatedAt: now, ExpiresAt: &future}},
			92.5,
		},
		{
			"a ban does not cap the score",
			92.5,
			[]*domain.ModerationAction{{Action: domain.ActionBan, CreatedAt: now}},
			92.5,
		},
		{
			"one live mute among expired history still caps",
			92.5,
			[]*domain.ModerationAction{
				{Action: domain.ActionWarn, CreatedAt: now},
				muteAction(past, &past),
				muteAction(now, &future),
			},
			capped,
		},
		{
			// The cap is an upper bound, not an assignment: someone already
			// further down must not be lifted up to it.
			"a score already below the cap is not raised",
			10, []*domain.ModerationAction{muteAction(now, &future)}, 10,
		},
		{
			"a nil action in the list is skipped rather than panicking",
			92.5, []*domain.ModerationAction{nil, muteAction(now, &future)}, capped,
		},
		{
			// The instant a mute expires it stops applying; ExpiresAt is
			// exclusive.
			"a mute expiring exactly now has lapsed",
			92.5, []*domain.ModerationAction{muteAction(past, &now)}, 92.5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := clampForActiveMute(tt.score, tt.actions, now); got != tt.want {
				t.Errorf("clampForActiveMute() = %v, want %v", got, tt.want)
			}
		})
	}
}

// Suspend and ban were checked before deciding not to clamp them, rather than
// assumed. Both are enforced through fields the recalculation never touches, so
// a restored score cannot revive either one. If that stops being true, this
// test fails and the clamp needs extending.
func TestSuspendAndBanSurviveARestoredScore(t *testing.T) {
	// The most generous score the composite can produce.
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
			if tt.user.CanPost() {
				t.Error("CanPost() = true after a full-score recalculation")
			}
			if tt.user.CanVouch() {
				t.Error("CanVouch() = true after a full-score recalculation")
			}
		})
	}
}

// The mute check is a required input, not a best-effort one: silently scoring a
// user as unmuted because the lookup failed would release them.
func TestCalcCompositeTrust_ActionLookupFailureAborts(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	dbDown := errors.New("db connection lost")
	inputs := mutedMember(now, nil, nil)
	inputs.actionsErr = dbDown

	score, err := CalcCompositeTrust(context.Background(), inputs, "muted", now)
	if !errors.Is(err, dbDown) {
		t.Fatalf("error = %v, want it to wrap %v", err, dbDown)
	}
	if score != 0 {
		t.Errorf("score = %v, want 0 alongside the error", score)
	}
}
