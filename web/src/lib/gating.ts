import type { User } from "../api/types";
import { POSTING_THRESHOLD, VOUCHING_THRESHOLD, canPost, canVouch } from "./trust";

/** The subset of a user these checks read. */
type Actor = Pick<User, "trust_score" | "role" | "is_active"> | null;

/**
 * blockReason explains, in the second person, why an actor cannot take a
 * trust-gated action — or returns null when they can.
 *
 * The order of the checks is the order of the causes: a suspended council
 * member is told they are suspended, not that their score is too low, because
 * the score is not what is standing in their way. It mirrors the order in
 * canPost/canVouch so the two cannot disagree about which rule bit first.
 */
function blockReason(user: Actor, threshold: number, action: string): string | null {
  if (!user) {
    return `You must be signed in to ${action}.`;
  }
  if (!user.is_active) {
    return `Your account is suspended, so you cannot ${action} right now.`;
  }
  if (user.role === "banned") {
    return `Your account has been banned, so you cannot ${action}.`;
  }
  if (user.role === "pending") {
    return `Your account is awaiting approval by the council. You will be able to ${action} once it is approved.`;
  }

  const score = user.trust_score;
  if (!Number.isFinite(score) || score < threshold) {
    // Displayed with floor, not round: a score of 29.7 against a threshold of
    // 30 must not render as "You need 30. Yours is 30", which reads like a bug
    // in the gate rather than an explanation of it.
    const shown = Number.isFinite(score) ? Math.floor(score) : 0;
    return `You need a trust score of ${threshold} to ${action}. Yours is ${shown}. Trust grows when other members vouch for you.`;
  }

  return null;
}

/**
 * postingBlockReason returns the message to show someone who cannot post yet,
 * or null when they can.
 *
 * It exists so the composer can say why it is disabled instead of letting
 * someone write a whole post and submit it into a guaranteed 403 — the server
 * stays the authority, but the user's effort is not wasted discovering that.
 *
 * Returns null exactly when canPost returns true; the two are tested against
 * each other so a future change to one cannot silently outgrow the other.
 */
export function postingBlockReason(user: Actor): string | null {
  return blockReason(user, POSTING_THRESHOLD, "post");
}

/**
 * vouchingBlockReason is the same contract for vouching, which sits behind a
 * higher threshold. Returns null exactly when canVouch returns true.
 */
export function vouchingBlockReason(user: Actor): string | null {
  return blockReason(user, VOUCHING_THRESHOLD, "vouch for someone");
}

/** Re-exported so callers need only one import to gate and explain. */
export { canPost, canVouch, POSTING_THRESHOLD, VOUCHING_THRESHOLD };
