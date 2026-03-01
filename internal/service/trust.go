package service

import "time"

// ActivePenalty represents a penalty still affecting a user's moderation score.
type ActivePenalty struct {
	Points    float64   // Original penalty points (direct or propagated)
	CreatedAt time.Time // When the penalty was applied
	DecayDays int       // Days until fully decayed (0 = permanent)
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
// vouches and the average trust score of those who vouched for the user.
// Each vouch adds 15 points to the base (capped at 100), then scaled by the
// average vouchee trust health.
func CalcVoucherScore(activeVouchCount int, avgVoucheeTrust float64) float64 {
	if activeVouchCount <= 0 {
		return 0
	}

	trust := max(0.0, min(100.0, avgVoucheeTrust))
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
