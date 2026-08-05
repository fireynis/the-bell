package service

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
)

// activityWindowDays is the rolling window the activity component measures over.
const activityWindowDays = 90

// ActivePenalty represents a penalty still affecting a user's moderation score.
type ActivePenalty struct {
	Points    float64   // Original penalty points (direct or propagated)
	CreatedAt time.Time // When the penalty was applied
	DecayDays int       // Days until fully decayed (0 = permanent)
}

// TrustInputs is everything the composite trust calculation needs to read.
//
// It is declared here, beside the calculator, rather than being folded into the
// PostRepository/ReactionRepository/VouchRepository interfaces: those are
// implemented by several narrow stubs that have no business growing methods
// they never call. A single concrete repository bundle satisfies this by method
// promotion.
type TrustInputs interface {
	GetUserByID(ctx context.Context, id string) (*domain.User, error)
	CountPostsByAuthorSince(ctx context.Context, authorID string, since time.Time) (int64, error)
	CountReactionsReceivedByAuthorSince(ctx context.Context, authorID string, since time.Time) (int64, error)
	CountActiveVouchesWithAvgTrust(ctx context.Context, voucheeID string) (int64, float64, error)
	ListActivePenaltiesByUser(ctx context.Context, userID string) ([]domain.TrustPenalty, error)
}

// TrustRecalcQueue enqueues a user for asynchronous trust recalculation.
//
// Recalculation is a background job, so every caller treats a failure here as
// non-fatal: the score goes stale, but the action that triggered it still
// stands.
type TrustRecalcQueue interface {
	EnqueueRecalc(ctx context.Context, userID string) error
}

// toActivePenalty converts a stored penalty into the form the moderation score
// works with.
//
// The stored row carries an absolute DecaysAt rather than a duration, but
// PropagatePenalties writes CreatedAt and DecaysAt from a single clock reading,
// so the decay window is exactly the difference between them.
//
// The delta is rounded, not truncated. DecaysAt comes from AddDate, which adds
// calendar days: across a daylight-saving transition a "90 day" window is
// 89.958 or 90.042 days of wall time, and truncating would quietly turn it into
// 89. A non-permanent penalty also never maps to 0, because
// CalcModerationScore reads 0 as permanent — the exact opposite meaning — so a
// window that rounds below a day is clamped to one day, after which it decays
// away normally.
func toActivePenalty(p domain.TrustPenalty) ActivePenalty {
	return ActivePenalty{
		Points:    p.PenaltyAmount,
		CreatedAt: p.CreatedAt,
		DecayDays: penaltyDecayDays(p),
	}
}

// penaltyDecayDays returns the decay window in days, or 0 for a permanent
// penalty.
func penaltyDecayDays(p domain.TrustPenalty) int {
	if p.DecaysAt == nil {
		return 0 // permanent
	}
	days := int(math.Round(p.DecaysAt.Sub(p.CreatedAt).Hours() / 24))
	if days < 1 {
		return 1
	}
	return days
}

// CalcCompositeTrust computes a user's trust score from the four weighted
// components. This is the model documented in the design doc and the user
// guide, and the one the score thresholds throughout the app assume.
func CalcCompositeTrust(ctx context.Context, inputs TrustInputs, userID string, now time.Time) (float64, error) {
	user, err := inputs.GetUserByID(ctx, userID)
	if err != nil {
		return 0, fmt.Errorf("looking up user: %w", err)
	}

	since := now.AddDate(0, 0, -activityWindowDays)

	recentPosts, err := inputs.CountPostsByAuthorSince(ctx, userID, since)
	if err != nil {
		return 0, fmt.Errorf("counting recent posts: %w", err)
	}

	recentReactions, err := inputs.CountReactionsReceivedByAuthorSince(ctx, userID, since)
	if err != nil {
		return 0, fmt.Errorf("counting recent reactions: %w", err)
	}

	vouchCount, avgVoucherTrust, err := inputs.CountActiveVouchesWithAvgTrust(ctx, userID)
	if err != nil {
		return 0, fmt.Errorf("counting active vouches: %w", err)
	}

	penalties, err := inputs.ListActivePenaltiesByUser(ctx, userID)
	if err != nil {
		return 0, fmt.Errorf("listing active penalties: %w", err)
	}

	active := make([]ActivePenalty, len(penalties))
	for i, p := range penalties {
		active[i] = toActivePenalty(p)
	}

	return CompositeScore(
		CalcTenureScore(user.JoinedAt, now),
		CalcActivityScore(int(recentPosts), int(recentReactions)),
		CalcVoucherScore(int(vouchCount), avgVoucherTrust),
		CalcModerationScore(active, now),
	), nil
}

// CalcTenureScore returns a score from 0-100 based on how long the user has
// been a member. Linearly scales from 0 at join to 100 at 365 days.
func CalcTenureScore(joinedAt time.Time, now time.Time) float64 {
	days := now.Sub(joinedAt).Hours() / 24
	if days < 0 {
		return 0
	}
	score := (days / 365.0) * 100.0
	return min(100.0, score)
}

// CalcActivityScore returns a score from 0-100 based on recent posting and
// reaction activity. Both inputs should be pre-filtered to a 90-day window.
// Posts contribute 50% (capped at 90 posts) and reactions 50% (capped at 270).
func CalcActivityScore(recentPosts int, reactionsReceived int) float64 {
	posts := max(0, recentPosts)
	reactions := max(0, reactionsReceived)

	postScore := min(100.0, (float64(posts)/90.0)*100.0) * 0.50
	reactionScore := min(100.0, (float64(reactions)/270.0)*100.0) * 0.50

	return postScore + reactionScore
}

// CalcVoucherScore returns a score from 0-100 based on the number of active
// vouches the user has RECEIVED and the average trust score of the people who
// vouched for them. Each vouch adds 15 points to the base (capped at 100),
// then scaled by the vouchers' average trust health, so an endorsement from a
// well-regarded neighbour is worth more than one from a marginal account.
//
// The direction matters and was previously ambiguous: the design doc describes
// this component as counting vouches GIVEN and averaging the vouchees' trust,
// but CountActiveVouchesWithAvgTrust filters on vouchee_id and joins the
// voucher, i.e. vouches received. The query and the user guide agree; the
// design doc is the outlier.
func CalcVoucherScore(activeVouchCount int, avgVoucherTrust float64) float64 {
	if activeVouchCount <= 0 {
		return 0
	}

	trust := max(0.0, min(100.0, avgVoucherTrust))
	base := min(100.0, float64(activeVouchCount)*15.0)
	health := trust / 100.0

	return base * health
}

// CalcModerationScore returns a score from 0-100 based on active penalties.
// Starts at 100 and subtracts remaining penalty points after linear decay.
// Permanent penalties (DecayDays == 0) never decay.
func CalcModerationScore(penalties []ActivePenalty, now time.Time) float64 {
	var totalPenalty float64
	for _, p := range penalties {
		if p.DecayDays == 0 {
			totalPenalty += p.Points
			continue
		}

		elapsed := now.Sub(p.CreatedAt).Hours() / 24
		if elapsed < 0 {
			// Penalty in the future — full points
			totalPenalty += p.Points
			continue
		}

		ratio := 1.0 - elapsed/float64(p.DecayDays)
		if ratio <= 0 {
			continue // fully decayed
		}
		totalPenalty += p.Points * ratio
	}

	return max(0, 100.0-totalPenalty)
}

// CompositeScore combines the four component scores into a single trust score.
// Weights: tenure 15%, activity 20%, voucher 35%, moderation 30%.
func CompositeScore(tenure, activity, voucher, moderation float64) float64 {
	score := tenure*0.15 + activity*0.20 + voucher*0.35 + moderation*0.30
	return max(0, min(100.0, score))
}
