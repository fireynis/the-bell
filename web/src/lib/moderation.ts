import type {
  ActionHistoryEntry,
  ApiError,
  MuteLift,
  MuteStatus,
  Post,
  TakeActionRequest,
  User,
} from "../api/types";
import { personName, shortId } from "./people";
import { byteLength, type ValidationResult } from "./post";

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

/**
 * Everything a severity decides, mirroring the four functions over severity in
 * internal/domain/moderation.go: DirectPenalty, PenaltyDecayDays,
 * PropagationDepth and PropagationDecay.
 *
 * They are one record here rather than four lookups because the dialog needs
 * all four to describe a single action, and because a severity the server does
 * not recognize should fail once — the Go side went to some trouble to stop an
 * unknown severity being answered with a zero from each map in turn.
 */
export interface SeverityConsequence {
  /** Trust points the person acted on loses. Mirrors DirectPenalty. */
  penalty: number;
  /**
   * Days until the penalty has decayed away, or null when it never does.
   * Mirrors PenaltyDecayDays, where a recognized 0 means permanent.
   */
  decayDays: number | null;
  /** How many hops out through the vouch graph it reaches. Mirrors PropagationDepth. */
  depth: number;
  /** The fraction of the penalty surviving each hop. Mirrors PropagationDecay. */
  hopSurvival: number;
}

const SEVERITY_CONSEQUENCES: Record<number, SeverityConsequence> = {
  1: { penalty: 5, decayDays: 90, depth: 1, hopSurvival: 0.5 },
  2: { penalty: 10, decayDays: 180, depth: 1, hopSurvival: 0.7 },
  3: { penalty: 25, decayDays: 270, depth: 2, hopSurvival: 0.6 },
  4: { penalty: 40, decayDays: 365, depth: 2, hopSurvival: 0.7 },
  5: { penalty: 100, decayDays: null, depth: 3, hopSurvival: 0.75 },
};

/** The word the Go comments use for each severity, 1 through 5. */
const SEVERITY_NAMES: Record<number, string> = {
  1: "Minor",
  2: "Moderate",
  3: "Serious",
  4: "Severe",
  5: "Ban-level",
};

/**
 * What each action type does to the person it lands on, beyond the trust
 * penalty every action carries.
 *
 * Drawn from planEnforcement in internal/service/moderation_action.go and from
 * what the enforced state actually blocks: a mute is checked by
 * domain.User.CanPost alone, while a suspension is folded into is_active and so
 * meets middleware.RequireActive on every guarded route.
 */
const ACTION_EFFECTS: Record<ActionType, string> = {
  warn: "Nothing is blocked. The warning goes on their moderation record, with the reason you write below.",
  mute: "They cannot post until the mute expires. Reading the feed, reacting and vouching are unaffected.",
  suspend:
    "They cannot post, react, vouch or report until the suspension expires. Reading the feed still works.",
  ban: "Their role becomes banned and their trust score is set to zero. They cannot post or vouch again, and a ban does not expire on its own.",
};

/**
 * severityConsequence looks up what a severity costs, or null when the severity
 * is not one the server recognizes.
 *
 * Null rather than a zeroed record, for the reason the Go side returns an `ok`
 * alongside each of these: a penalty of no points, reaching nobody, decaying
 * never, is a plausible-looking answer that is wrong in every direction.
 */
export function severityConsequence(severity: number): SeverityConsequence | null {
  const c = SEVERITY_CONSEQUENCES[severity];
  // A copy, on the same principle as severitiesFor: a caller cannot edit the
  // policy by holding onto what it was told.
  return c ? { ...c } : null;
}

/**
 * Points as a moderator should read them: 28, not 28.000000000000004, which is
 * what 40 * 0.7 is in binary floating point.
 */
function formatPoints(points: number): string {
  const rounded = Math.round(points * 10) / 10;
  return Number.isInteger(rounded) ? String(rounded) : rounded.toFixed(1);
}

/**
 * How to name an action inside a sentence. A warning is named by its severity
 * because that is the one action where the moderator picks it; the other three
 * carry exactly one severity each, so naming it would only repeat the action.
 */
function actionNoun(actionType: ActionType, severity: number): string {
  switch (actionType) {
    case "warn": {
      const name = SEVERITY_NAMES[severity];
      return name ? `a ${name.toLowerCase()} warning` : "a warning";
    }
    case "mute":
      return "a mute";
    case "suspend":
      return "a suspension";
    case "ban":
      return "a ban";
  }
}

/** What taking an action will do, in the words shown to the moderator. */
export interface ActionConsequence {
  /** The action named for a sentence, e.g. "a minor warning" or "a ban". */
  noun: string;
  /** What the action does to the person it lands on. */
  effect: string;
  /** The trust penalty they take, and how long it lingers. */
  penalty: string;
  /** How far that penalty travels through the people who vouched for them. */
  propagation: string;
}

/**
 * actionConsequence describes what an action will do, so a volunteer councilor
 * is not choosing between "Level 1" and "Level 5" with nothing to tell them
 * apart.
 *
 * The propagation sentence is the reason this exists. Acting on somebody also
 * takes trust from everyone who vouched for them, out to three hops for a ban —
 * that is the whole point of a vouch graph, and it appeared nowhere in the
 * interface that applies it.
 *
 * An action type and severity the server would reject yield null rather than a
 * description, so the dialog stays silent instead of promising an outcome that
 * cannot happen. The pairing is checked against severitiesFor, which mirrors
 * allowedSeverity in internal/service/moderation_action.go.
 */
export function actionConsequence(actionType: string, severity: number): ActionConsequence | null {
  if (!severitiesFor(actionType).includes(severity)) return null;

  const c = severityConsequence(severity);
  if (!c) return null;

  const perHop = `${Math.round(c.hopSurvival * 100)}%`;
  const firstHop = formatPoints(c.penalty * c.hopSurvival);

  return {
    noun: actionNoun(actionType as ActionType, severity),
    effect: ACTION_EFFECTS[actionType as ActionType],
    penalty:
      c.decayDays === null
        ? `It costs them ${formatPoints(c.penalty)} trust points, which never decay.`
        : `It costs them ${formatPoints(c.penalty)} trust points, which decay away over ${c.decayDays} days.`,
    propagation:
      c.depth === 1
        ? `This also lowers the standing of everyone who vouched for them, one step out: each of them loses ${firstHop} points. It travels no further.`
        : `This also lowers the standing of everyone who vouched for them, up to ${c.depth} steps out through the vouch graph. Anyone who vouched for them directly loses ${firstHop} points, and each step further out carries ${perHop} of the step before it.`,
  };
}

/**
 * severityChoiceLabel names a severity in a picker, e.g. "Minor — 5 trust
 * points". "Level 1" through "Level 5" told a moderator nothing about which one
 * to choose, and the number is the input to the penalty rather than a rank
 * anybody could interpret.
 */
export function severityChoiceLabel(severity: number): string {
  const c = severityConsequence(severity);
  const name = SEVERITY_NAMES[severity] ?? `Level ${severity}`;
  return c ? `${name} — ${formatPoints(c.penalty)} trust points` : name;
}

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
  const bytes = byteLength(trimmed);
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

/**
 * describeHistorySubject names the member whose moderation history is on
 * screen, for the heading of that page.
 *
 * There is no read that hands the page a member's profile — it loads a list of
 * actions and nothing else — so the name comes from the actions themselves,
 * every one of which is addressed to this member and carries
 * `target_display_name`. Any entry will do; the first one with a name wins, so
 * a member named on some rows and not others is still named.
 *
 * This is only sound for the default listing, which is actions taken *against*
 * the user. The `role=moderator` variant lists actions they took, where the
 * subject is the moderator rather than the target — this page does not ask for
 * that variant, and would need the other field if it did.
 *
 * The short id stays alongside the name rather than being replaced by it. Two
 * members may share a display name, and a moderator about to act on somebody
 * needs to know which of them they are reading. When there is no name it is all
 * there is to show, and it is not repeated.
 */
export function describeHistorySubject(
  entries: readonly ActionHistoryEntry[] | null | undefined,
  userId: string,
): string {
  const named = entries?.find((entry) => entry?.action?.target_display_name?.trim());
  const name = personName(named?.action?.target_display_name, userId);
  const short = shortId(userId);

  return name === short ? short : `${name} · ${short}`;
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
