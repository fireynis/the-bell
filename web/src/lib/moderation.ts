import type { ApiError, MuteLift, MuteStatus, Post, TakeActionRequest, User } from "../api/types";
import type { ValidationResult } from "./post";

/** Mirrors the ActionType constants in internal/domain/moderation.go. */
export const ACTION_TYPES = ["warn", "mute", "suspend", "ban"] as const;

export type ActionType = (typeof ACTION_TYPES)[number];

/**
 * Mirrors allowedSeverity in internal/service/moderation_action.go. Severity is
 * bound to the action type because it is what drives the trust penalty and how
 * far through the vouch graph it propagates — a "warn" carrying ban-level
 * severity would be rejected by the server.
 */
const ALLOWED_SEVERITY: Record<ActionType, readonly number[]> = {
  warn: [1, 2],
  mute: [3],
  suspend: [4],
  ban: [5],
};

/**
 * Mirrors validateActionRequest in internal/service/moderation_action.go:
 * mutes and suspensions are temporary and must carry a duration, while bans are
 * permanent and are rejected if one is sent.
 */
const NEEDS_DURATION: readonly ActionType[] = ["mute", "suspend"];

/** Mirrors maxActionReasonLen in internal/service/moderation_action.go. */
export const MAX_ACTION_REASON_LENGTH = 1000;

/** Mirrors maxRemovalReasonLen in internal/service/post.go. */
export const MAX_REMOVAL_REASON_LENGTH = 1000;

/**
 * validateRemovalReason applies the server's rules for a moderator's post
 * removal note so the dialog can disable its submit button instead of
 * round-tripping to a guaranteed 400.
 *
 * A reason is mandatory here, unlike an author deleting their own post: it is
 * the only record of why a moderator overrode a member's speech.
 *
 * The length is measured in UTF-8 bytes because the server's check is
 * `len(reason)` on a Go string. Measuring JS string length would wave through a
 * reason of multi-byte characters that the server then rejects.
 */
export function validateRemovalReason(reason: string): ValidationResult {
  const trimmed = (reason ?? "").trim();
  if (trimmed.length === 0) {
    return { valid: false, error: "A reason is required." };
  }
  const bytes = new TextEncoder().encode(trimmed).length;
  if (bytes > MAX_REMOVAL_REASON_LENGTH) {
    return {
      valid: false,
      error: `Reason is ${bytes} bytes; the maximum is ${MAX_REMOVAL_REASON_LENGTH}.`,
    };
  }
  return { valid: true };
}

/**
 * canRemovePost reports whether the queue should offer to take a post down.
 *
 * Mirrors canRemoveByModerator in internal/service/post.go: only a visible post
 * may be removed, so offering the control on one that is already gone would
 * guarantee a 400. A null post is one the card has not loaded yet, or failed
 * to — either way there is nothing to act on.
 */
export function canRemovePost(post: Post | null | undefined): boolean {
  return post?.status === "visible";
}

/** What the queue should do with a report after acting on it. */
export interface ResolutionResult {
  /** Whether the report may be dropped from the queue. */
  resolved: boolean;
  /** Why it could not be, present only when `resolved` is false. */
  error?: string;
}

/**
 * reportResolutionOutcome decides what the queue does after it has tried to
 * mark a report reviewed.
 *
 * Taking action used to drop the report from local state and never tell the
 * server, so it came back `pending` on the next load and moderators re-reviewed
 * work they had already done. Resolving it is a second request, and this decides
 * what happens when that request is the thing that fails.
 *
 * A 404 still resolves: another moderator got there first, so the report is
 * gone either way and holding it in this moderator's queue helps nobody. This
 * matches how ReportCard already treats a 404 when dismissing.
 *
 * Any other failure does NOT resolve. The action or removal already succeeded,
 * so the report is genuinely still pending on the server — dropping it from the
 * queue anyway is exactly the divergence this function exists to stop, and the
 * message says the action stuck so the moderator does not redo it.
 */
export function reportResolutionOutcome(err: ApiError | null): ResolutionResult {
  if (err === null) {
    return { resolved: true };
  }
  if (err.status === 404) {
    return { resolved: true };
  }
  const detail = err.error ? `: ${err.error}` : ".";
  return {
    resolved: false,
    error: `The action went through, but the report could not be marked reviewed${detail} It is still in the queue.`,
  };
}

/** The largest duration the dialog offers, one year in hours. */
export const MAX_DURATION_HOURS = 8760;

const SECONDS_PER_HOUR = 3600;

function isActionType(value: string): value is ActionType {
  return (ACTION_TYPES as readonly string[]).includes(value);
}

/**
 * severitiesFor lists the severities valid for an action type, in the order the
 * picker should offer them — the first is the one to preselect.
 *
 * An unknown action type yields an empty list rather than throwing, so a server
 * that starts sending a new action type cannot break the dialog.
 */
export function severitiesFor(actionType: string): number[] {
  return isActionType(actionType) ? [...ALLOWED_SEVERITY[actionType]] : [];
}

/** needsDuration reports whether an action type is a temporary one. */
export function needsDuration(actionType: string): boolean {
  return isActionType(actionType) && NEEDS_DURATION.includes(actionType);
}

export interface ActionInput {
  actionType: string;
  severity: number;
  reason: string;
  /** The signed-in moderator taking the action. */
  moderatorId: string;
  /** The member the action lands on. */
  targetUserId: string;
}

/**
 * validateAction applies the server's rules so the dialog can disable its
 * submit button instead of round-tripping to a guaranteed 400.
 *
 * durationHours is only consulted for the action types that need one; passing
 * it is optional so callers that have not collected a duration yet can still
 * check the rest of the form.
 *
 * The two ids are part of the input rather than extra parameters because
 * buildActionRequest needs the target too, and one struct means the request
 * cannot be built for a different person than the one just validated.
 */
export function validateAction(input: ActionInput, durationHours?: number): ValidationResult {
  // Checked before every field, and deliberately earlier than the server checks
  // it — validateActionRequest in internal/service/moderation_action.go
  // validates type, severity and reason first. Those are all things the
  // moderator can fix in this form; acting on themselves is not, so reporting a
  // severity or reason problem first would send them to correct fields that
  // were never the reason the action could not go through. It is also the more
  // specific answer, the same reasoning that puts the self-check ahead of the
  // trust gate in vouchBlockReason.
  //
  // Both ids must be present to count as a match: a viewer whose identity has
  // not loaded yet has two empty strings, and that is unknown, not self.
  if (input?.moderatorId && input.moderatorId === input?.targetUserId) {
    return { valid: false, error: "You cannot moderate yourself." };
  }

  if (!input || !isActionType(input.actionType)) {
    return { valid: false, error: "Select an action type." };
  }

  if (!ALLOWED_SEVERITY[input.actionType].includes(input.severity)) {
    return {
      valid: false,
      error: `Severity ${input.severity} is not valid for a ${input.actionType}.`,
    };
  }

  const reason = (input.reason ?? "").trim();
  if (reason.length === 0) {
    return { valid: false, error: "A reason is required." };
  }
  if (reason.length > MAX_ACTION_REASON_LENGTH) {
    return {
      valid: false,
      error: `Reason is ${reason.length} characters; the maximum is ${MAX_ACTION_REASON_LENGTH}.`,
    };
  }

  if (needsDuration(input.actionType)) {
    if (!Number.isFinite(durationHours) || (durationHours as number) <= 0) {
      return { valid: false, error: `A ${input.actionType} needs a duration in hours.` };
    }
    if ((durationHours as number) > MAX_DURATION_HOURS) {
      return {
        valid: false,
        error: `Duration must be at most ${MAX_DURATION_HOURS} hours.`,
      };
    }
  }

  return { valid: true };
}

/**
 * buildActionRequest assembles the request body.
 *
 * The target is read off the same input validateAction was given rather than
 * passed separately, so the request cannot be addressed to someone other than
 * the person the self-moderation check cleared.
 *
 * The duration is omitted entirely for action types that do not take one: the
 * server rejects a ban that carries a duration, so sending a stale value left
 * in the form field would fail the whole action.
 */
export function buildActionRequest(
  input: ActionInput,
  durationHours?: number,
): TakeActionRequest {
  return {
    target_user_id: input?.targetUserId ?? "",
    action_type: input.actionType,
    severity: input.severity,
    reason: (input.reason ?? "").trim(),
    duration_seconds: needsDuration(input.actionType)
      ? Math.round((durationHours as number) * SECONDS_PER_HOUR)
      : undefined,
  };
}

/**
 * activeMuteExpiry reads GET /api/v1/moderation/users/{id}/mute and reports
 * when the mute ends, or null when there is no mute in force.
 *
 * The server already omits muted_until for a user who is not muted and for one
 * whose mute has expired, so an absent field is the answer to "is this person
 * muted?" — the same rule the caller's own profile uses. The expiry is
 * re-checked against `now` anyway, because a page left open outlives the
 * response that loaded it and would otherwise keep offering to lift a mute that
 * has since run out on its own.
 *
 * A timestamp that cannot be parsed reads as no mute rather than an Invalid
 * Date, matching formatDateTime's contract: the moderator sees the plain view
 * instead of a control claiming to lift something unnamed.
 */
export function activeMuteExpiry(
  status: MuteStatus | null | undefined,
  now: Date,
): Date | null {
  return parseActiveExpiry(status?.muted_until, now);
}

/**
 * liftMuteBlockReason mirrors canLiftMute in
 * internal/service/moderation_action.go so the page can explain itself instead
 * of round-tripping to a guaranteed 400.
 *
 * A moderator may not lift a mute placed on themselves: that is the one case no
 * route guard can catch, since a muted moderator satisfies every middleware in
 * the chain. Both ids must be present to count as a match — a viewer whose
 * identity has not loaded yet has two empty strings, and that is unknown, not
 * self, exactly as validateAction treats it.
 */
export function liftMuteBlockReason(viewerId: string, targetId: string): string | null {
  if (viewerId && viewerId === targetId) {
    return "You cannot moderate yourself.";
  }
  return null;
}

/**
 * What a member's own profile has to say about their moderation state.
 *
 * A mute and a lift are separate facts and both can be true at once: being
 * muted again does not undo having been released from an earlier mute, so
 * collapsing them into one status would hide half the record.
 */
export interface OwnMuteNotice {
  /** When an active mute ends, or null when none is in force. */
  mutedUntil: Date | null;
  /** The most recent early release, or null when there has never been one. */
  latestLift: MuteLift | null;
  /** Whether there is anything at all to show, so the caller can skip the UI. */
  hasAnything: boolean;
}

/**
 * ownMuteNotice reads the moderation half of the caller's own profile.
 *
 * It exists because muted_until disappears the moment a mute is lifted. Without
 * the lift record a member released early would see their profile simply stop
 * saying they were muted, with nothing anywhere to say it happened — the whole
 * moderation audit trail sits behind the moderator-only /v1/moderation routes.
 *
 * The expiry is re-checked against `now` even though the server already omits
 * an expired muted_until: a page left open outlives the response that loaded
 * it, and would otherwise go on telling a member they cannot post. An
 * unparseable timestamp reads as no mute rather than an Invalid Date, matching
 * activeMuteExpiry.
 *
 * Another member's profile carries neither field, so it reports nothing — this
 * is never a way to learn about somebody else's moderation.
 */
export function ownMuteNotice(user: User | null | undefined, now: Date): OwnMuteNotice {
  const mutedUntil = parseActiveExpiry(user?.muted_until, now);
  const latestLift = user?.mute_lifts?.[0] ?? null;

  return {
    mutedUntil,
    latestLift,
    hasAnything: mutedUntil !== null || latestLift !== null,
  };
}

/** Shared by ownMuteNotice and activeMuteExpiry: a timestamp still in the future, or null. */
function parseActiveExpiry(raw: string | undefined, now: Date): Date | null {
  if (!raw) return null;

  const expiry = new Date(raw);
  if (Number.isNaN(expiry.getTime())) return null;

  return expiry.getTime() > now.getTime() ? expiry : null;
}
