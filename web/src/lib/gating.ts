import type { User } from "../api/types";
import { POSTING_THRESHOLD, VOUCHING_THRESHOLD, canPost, canVouch } from "./trust";

/** The subset of a user these checks read. */
export type Actor = Pick<User, "trust_score" | "role" | "is_active"> | null;

/**
 * Why there is no actor to check, in the case where that is not simply "nobody
 * is signed in".
 *
 * A null actor has two very different causes. Either there is no Kratos session
 * at all, or there is one and GET /users/me did not answer — and AuthContext
 * used to collapse both into `user = null`, after which a signed-in member was
 * told they must sign in. Telling someone to do the thing they have already
 * done sends them round a loop that cannot end, so the caller passes what it
 * knows and the message can be true.
 */
export interface ActorContext {
  /** True when a session exists but the member's profile could not be read. */
  profileUnavailable?: boolean;
}

/**
 * How a pending member becomes a full one.
 *
 * Both paths are real and neither is the whole story: any member's vouch
 * promotes a pending user on the spot (VouchService.Vouch), and while the town
 * is still small enough to be in bootstrap the council can approve them
 * directly (ApprovalService.Approve). The gate line and the welcome banner
 * share this sentence so the two cannot end up describing different towns.
 */
export const PENDING_PATH_TO_MEMBER =
  "one neighbour vouching for you, or council approval while the town is still starting up";

/**
 * The welcome a member sees while the town has not rung them in yet.
 *
 * It lives here beside the gate line rather than inside the banner component
 * because it is the long form of the same fact, and because what a pending
 * member is told is a policy question rather than a layout one.
 */
export const PENDING_WELCOME = {
  title: "Welcome — the town has not rung you in yet",
  meaning:
    "Your name is on the roll, but none of your neighbours have vouched for you yet, so the bell is not yours to ring for the moment.",
  next: `It takes ${PENDING_PATH_TO_MEMBER}. There is nothing to apply for — someone here simply has to say they know you.`,
  meanwhile:
    "Until then you can read everything in the feed. Filling in your profile helps: a photo and a line about yourself give a neighbour something to recognise you by.",
  profileCta: "Complete your profile",
} as const;

/**
 * awaitingWelcome reports whether to greet this actor as a newcomer.
 *
 * A suspended pending account is deliberately excluded. The suspension is the
 * more specific and more useful thing to tell them, and a warm "welcome, a
 * neighbour will vouch for you soon" over the top of it would be false comfort.
 */
export function awaitingWelcome(user: Actor): boolean {
  return user !== null && user.role === "pending" && user.is_active;
}

/**
 * blockReason explains, in the second person, why an actor cannot take a
 * trust-gated action — or returns null when they can.
 *
 * The order of the checks is the order of the causes: a suspended council
 * member is told they are suspended, not that their score is too low, because
 * the score is not what is standing in their way. It mirrors the order in
 * canPost/canVouch so the two cannot disagree about which rule bit first.
 */
function blockReason(
  user: Actor,
  threshold: number,
  action: string,
  ctx?: ActorContext,
): string | null {
  if (!user) {
    if (ctx?.profileUnavailable) {
      return `We could not load your account just now, so we cannot tell whether you can ${action}. Refresh the page to try again.`;
    }
    return `You must be signed in to ${action}.`;
  }
  if (!user.is_active) {
    return `Your account is suspended, so you cannot ${action} right now.`;
  }
  if (user.role === "banned") {
    return `Your account has been banned, so you cannot ${action}.`;
  }
  if (user.role === "pending") {
    return `The town has not rung you in yet. It takes ${PENDING_PATH_TO_MEMBER} — then you can ${action}.`;
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
 * each other so a future change to one cannot silently outgrow the other. The
 * context only ever changes the wording for a null actor, which canPost already
 * refuses, so passing one cannot open the gate.
 */
export function postingBlockReason(user: Actor, ctx?: ActorContext): string | null {
  return blockReason(user, POSTING_THRESHOLD, "post", ctx);
}

/**
 * vouchingBlockReason is the same contract for vouching, which sits behind a
 * higher threshold. Returns null exactly when canVouch returns true.
 */
export function vouchingBlockReason(user: Actor, ctx?: ActorContext): string | null {
  return blockReason(user, VOUCHING_THRESHOLD, "vouch for someone", ctx);
}

/** Re-exported so callers need only one import to gate and explain. */
export { canPost, canVouch, POSTING_THRESHOLD, VOUCHING_THRESHOLD };
