import type { TakeActionRequest } from "../api/types";
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
}

/**
 * validateAction applies the server's rules so the dialog can disable its
 * submit button instead of round-tripping to a guaranteed 400.
 *
 * durationHours is only consulted for the action types that need one; passing
 * it is optional so callers that have not collected a duration yet can still
 * check the rest of the form.
 */
export function validateAction(input: ActionInput, durationHours?: number): ValidationResult {
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
 * The duration is omitted entirely for action types that do not take one: the
 * server rejects a ban that carries a duration, so sending a stale value left
 * in the form field would fail the whole action.
 */
export function buildActionRequest(
  targetUserId: string,
  input: ActionInput,
  durationHours?: number,
): TakeActionRequest {
  return {
    target_user_id: targetUserId,
    action_type: input.actionType,
    severity: input.severity,
    reason: (input.reason ?? "").trim(),
    duration_seconds: needsDuration(input.actionType)
      ? Math.round((durationHours as number) * SECONDS_PER_HOUR)
      : undefined,
  };
}
