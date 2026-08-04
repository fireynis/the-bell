import type { ProposalSummary } from "../api/types";

/**
 * approvalPercent reports the share of the council that has voted to approve,
 * as a 0-100 number for a progress bar's width.
 *
 * total_council is the divisor and can legitimately be zero — a town whose
 * council seats are vacant still has proposals on file — so the guard lives
 * here rather than at each call site. Without it the division yields NaN, which
 * React interpolates into the style as the literal "NaN%"; the browser drops
 * the invalid width and the bar silently collapses.
 *
 * The result is clamped because a stale or double-counted tally can report more
 * approvals than there are seats, which would otherwise render a bar wider than
 * its own track.
 */
export function approvalPercent(
  proposal: Pick<ProposalSummary, "approve_count" | "total_council">,
): number {
  const approvals = proposal?.approve_count;
  const council = proposal?.total_council;

  if (!Number.isFinite(approvals) || !Number.isFinite(council) || council <= 0) {
    return 0;
  }
  if (approvals <= 0) return 0;

  return Math.min(100, (approvals / council) * 100);
}

/**
 * applyVote folds a freshly-voted proposal back into the list on screen,
 * dropping any proposal that is no longer open.
 *
 * Casting the deciding vote closes a proposal, so the response can come back
 * already approved or rejected. Replacing the row in place would leave a
 * decided proposal sitting in a list captioned as open, still offering vote
 * buttons that would now be rejected — so the replace and the filter belong
 * together rather than as two steps a caller might do only half of.
 *
 * A proposal that is not in the list is not inserted: the list is the set of
 * proposals this council member was shown, and a vote on something outside it
 * is not a reason to grow it.
 */
export function applyVote(
  proposals: readonly ProposalSummary[],
  updated: ProposalSummary | null | undefined,
): ProposalSummary[] {
  const current = proposals ?? [];

  if (!updated || typeof updated.proposal_id !== "string") {
    return current.filter((p) => p?.status === "pending");
  }

  return current
    .map((p) => (p?.proposal_id === updated.proposal_id ? updated : p))
    .filter((p) => p?.status === "pending");
}
