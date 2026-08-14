import { describe, expect, it } from "vitest";
import {
  ACTION_TYPES,
  MAX_ACTION_REASON_LENGTH,
  MAX_DURATION_HOURS,
  MAX_REMOVAL_REASON_LENGTH,
  actionConsequence,
  activeMuteExpiry,
  buildActionRequest,
  canRemovePost,
  describeHistorySubject,
  severityChoiceLabel,
  severityConsequence,
  liftMuteBlockReason,
  needsDuration,
  ownMuteNotice,
  reportResolutionOutcome,
  severitiesFor,
  validateAction,
  validateRemovalReason,
  type ActionInput,
} from "./moderation";
import type { ActionHistoryEntry, ApiError, Post, User } from "../api/types";

const MODERATOR = "moderator-1";
const TARGET = "target-1";

function input(overrides: Partial<ActionInput> = {}): ActionInput {
  return {
    actionType: "warn",
    severity: 1,
    reason: "Repeated spam",
    moderatorId: MODERATOR,
    targetUserId: TARGET,
    ...overrides,
  };
}

describe("severitiesFor", () => {
  it.each([
    ["warn", [1, 2]],
    ["mute", [3]],
    ["suspend", [4]],
    ["ban", [5]],
  ])("allows %s at %o", (actionType, want) => {
    expect(severitiesFor(actionType)).toEqual(want);
  });

  // The dialog preselects the first entry, so the order is part of the contract.
  it("offers the mildest severity first, so that is what gets preselected", () => {
    expect(severitiesFor("warn")[0]).toBe(1);
  });

  it.each(["shadowban", "", "WARN"])("returns nothing for the unknown type %o", (actionType) => {
    expect(severitiesFor(actionType)).toEqual([]);
  });

  it("hands out a copy, so a caller cannot edit the policy", () => {
    severitiesFor("warn").push(99);
    expect(severitiesFor("warn")).toEqual([1, 2]);
  });

  // Severity drives the trust penalty and how far it propagates, so every
  // action type must map to at least one.
  it("covers every action type the dialog offers", () => {
    for (const actionType of ACTION_TYPES) {
      expect(severitiesFor(actionType).length).toBeGreaterThan(0);
    }
  });
});

// These numbers are the ones internal/domain/moderation.go applies. A dialog
// that quotes them wrongly is worse than one that says nothing, so they are
// pinned against the Go source rather than against each other.
describe("severityConsequence", () => {
  it.each([
    [1, 5, 90, 1, 0.5],
    [2, 10, 180, 1, 0.7],
    [3, 25, 270, 2, 0.6],
    [4, 40, 365, 2, 0.7],
  ])(
    "mirrors severity %i: %i points, %i days, %i hops, %f per hop",
    (severity, penalty, decayDays, depth, hopSurvival) => {
      expect(severityConsequence(severity)).toEqual({ penalty, decayDays, depth, hopSurvival });
    },
  );

  // PenaltyDecayDays returns 0 for a ban and relies on its second return value
  // to say that means permanent. Null carries that here, because 0 days would
  // read as "gone immediately" — the opposite of what it means.
  it("records a ban's penalty as never decaying", () => {
    expect(severityConsequence(5)).toEqual({
      penalty: 100,
      decayDays: null,
      depth: 3,
      hopSurvival: 0.75,
    });
  });

  it.each([0, 6, -1, 1.5, Number.NaN])(
    "refuses the unrecognized severity %o rather than answering with zeros",
    (severity) => {
      expect(severityConsequence(severity)).toBeNull();
    },
  );

  it("covers every severity the dialog can offer", () => {
    for (const actionType of ACTION_TYPES) {
      for (const severity of severitiesFor(actionType)) {
        expect(severityConsequence(severity)).not.toBeNull();
      }
    }
  });
});

describe("actionConsequence", () => {
  it("describes every action the dialog offers", () => {
    for (const actionType of ACTION_TYPES) {
      for (const severity of severitiesFor(actionType)) {
        const c = actionConsequence(actionType, severity);
        expect(c).not.toBeNull();
        expect(c?.effect.length).toBeGreaterThan(0);
        expect(c?.penalty).toContain("trust points");
        expect(c?.propagation).toContain("vouched");
      }
    }
  });

  // The pairing rules are the server's, so a combination it would reject gets
  // no description at all — describing an outcome that cannot happen is the
  // one thing worse than describing none.
  it.each([
    ["warn", 5],
    ["ban", 1],
    ["mute", 4],
    ["shadowban", 3],
    ["", 1],
  ])("says nothing about %s at severity %i, which the server refuses", (actionType, severity) => {
    expect(actionConsequence(actionType, severity)).toBeNull();
  });

  it("names a warning by its severity, since that is the choice being made", () => {
    expect(actionConsequence("warn", 1)?.noun).toBe("a minor warning");
    expect(actionConsequence("warn", 2)?.noun).toBe("a moderate warning");
  });

  it.each([
    ["mute", 3, "a mute"],
    ["suspend", 4, "a suspension"],
    ["ban", 5, "a ban"],
  ])("names %s plainly, since its severity is not a choice", (actionType, severity, noun) => {
    expect(actionConsequence(actionType, severity)?.noun).toBe(noun);
  });

  it("says a warning blocks nothing", () => {
    expect(actionConsequence("warn", 1)?.effect).toContain("Nothing is blocked");
  });

  // A mute is checked by domain.User.CanPost alone; a suspension is folded into
  // is_active and so meets RequireActive on every guarded route. Telling a
  // moderator the two are the same would have them reach for the wrong one.
  it("distinguishes what a mute blocks from what a suspension blocks", () => {
    expect(actionConsequence("mute", 3)?.effect).toContain("vouching are unaffected");
    expect(actionConsequence("suspend", 4)?.effect).toContain("vouch");
    expect(actionConsequence("suspend", 4)?.effect).toContain("until the suspension expires");
  });

  it("says a ban zeroes their trust and does not expire", () => {
    const effect = actionConsequence("ban", 5)?.effect ?? "";
    expect(effect).toContain("zero");
    expect(effect).toContain("does not expire");
  });

  it("gives the decay window for a penalty that fades", () => {
    expect(actionConsequence("mute", 3)?.penalty).toContain("270 days");
  });

  it("says outright that a ban's penalty never decays", () => {
    expect(actionConsequence("ban", 5)?.penalty).toContain("never decay");
  });

  // The whole reason this data exists: nothing in the dialog said that acting
  // on somebody also costs the people who vouched for them.
  it("tells a moderator how far a ban reaches through the vouch graph", () => {
    const propagation = actionConsequence("ban", 5)?.propagation ?? "";
    expect(propagation).toContain("everyone who vouched for them");
    expect(propagation).toContain("3 steps out");
  });

  it("says where a one-hop penalty stops rather than implying it keeps going", () => {
    const propagation = actionConsequence("warn", 1)?.propagation ?? "";
    expect(propagation).toContain("one step out");
    expect(propagation).toContain("no further");
  });

  // penalty * decayRate^depth, per planPropagatedPenalties. The first hop is
  // the number a moderator can check against a real voucher's score.
  it.each([
    ["warn", 1, "2.5"],
    ["warn", 2, "7"],
    ["mute", 3, "15"],
    ["suspend", 4, "28"],
    ["ban", 5, "75"],
  ])("quotes the first-hop cost of %s at severity %i as %s", (actionType, severity, points) => {
    expect(actionConsequence(actionType, severity)?.propagation).toContain(`${points} points`);
  });

  // 40 * 0.7 is 28.000000000000004 in binary floating point, and a dialog that
  // says so reads as broken.
  it("never shows a floating-point tail", () => {
    for (const actionType of ACTION_TYPES) {
      for (const severity of severitiesFor(actionType)) {
        const c = actionConsequence(actionType, severity);
        expect(`${c?.penalty} ${c?.propagation}`).not.toMatch(/\d\.\d{2,}/);
      }
    }
  });
});

describe("severityChoiceLabel", () => {
  // "Level 1" and "Level 2" gave a moderator nothing to choose between.
  it("names the severity and what it costs", () => {
    expect(severityChoiceLabel(1)).toBe("Minor — 5 trust points");
    expect(severityChoiceLabel(2)).toBe("Moderate — 10 trust points");
  });

  it("falls back to the bare level for a severity it does not know", () => {
    expect(severityChoiceLabel(9)).toBe("Level 9");
  });
});

describe("needsDuration", () => {
  it.each([
    ["mute", true],
    ["suspend", true],
    ["warn", false],
    ["ban", false],
    ["shadowban", false],
    ["", false],
  ])("%s -> %s", (actionType, want) => {
    expect(needsDuration(actionType)).toBe(want);
  });
});

describe("validateAction", () => {
  it("accepts a well-formed warning", () => {
    expect(validateAction(input())).toEqual({ valid: true });
  });

  it("accepts a mute with a duration", () => {
    expect(validateAction(input({ actionType: "mute", severity: 3 }), 24)).toEqual({
      valid: true,
    });
  });

  it("refuses before an action type is chosen", () => {
    expect(validateAction(input({ actionType: "" })).valid).toBe(false);
  });

  it("refuses an action type the server does not know", () => {
    expect(validateAction(input({ actionType: "shadowban" })).valid).toBe(false);
  });

  // Severity is bound to the action type on the server; a warn carrying
  // ban-level severity is a 400, not a harsher warning.
  it("refuses ban-level severity on a warning", () => {
    const result = validateAction(input({ severity: 5 }));
    expect(result.valid).toBe(false);
    expect(result.error).toContain("warn");
  });

  it("refuses warn-level severity on a mute", () => {
    expect(validateAction(input({ actionType: "mute", severity: 1 }), 24).valid).toBe(false);
  });

  it.each([1, 2])("accepts severity %d on a warning", (severity) => {
    expect(validateAction(input({ severity })).valid).toBe(true);
  });

  it("refuses severity zero, which is what an untouched picker holds", () => {
    expect(validateAction(input({ severity: 0 })).valid).toBe(false);
  });

  it("refuses an empty reason", () => {
    expect(validateAction(input({ reason: "" })).valid).toBe(false);
  });

  it("refuses a whitespace-only reason, which the server trims to empty", () => {
    expect(validateAction(input({ reason: "  \n\t " })).valid).toBe(false);
  });

  it("accepts a reason of exactly the maximum length", () => {
    expect(validateAction(input({ reason: "a".repeat(MAX_ACTION_REASON_LENGTH) })).valid).toBe(
      true,
    );
  });

  it("refuses one character over the maximum", () => {
    const result = validateAction(input({ reason: "a".repeat(MAX_ACTION_REASON_LENGTH + 1) }));
    expect(result.valid).toBe(false);
    expect(result.error).toContain(String(MAX_ACTION_REASON_LENGTH));
  });

  // The server measures the trimmed reason, so padding must not push a
  // borderline reason over.
  it("measures the reason after trimming", () => {
    const reason = `  ${"a".repeat(MAX_ACTION_REASON_LENGTH)}  `;
    expect(validateAction(input({ reason })).valid).toBe(true);
  });

  it.each(["mute", "suspend"])("refuses a %s with no duration", (actionType) => {
    const severity = actionType === "mute" ? 3 : 4;
    expect(validateAction(input({ actionType, severity })).valid).toBe(false);
  });

  it.each([0, -1])("refuses a duration of %d hours", (hours) => {
    expect(validateAction(input({ actionType: "mute", severity: 3 }), hours).valid).toBe(false);
  });

  // An emptied number input yields NaN, which would serialise to null and be
  // rejected by the server as a missing duration.
  it("refuses a duration that is not a number", () => {
    expect(validateAction(input({ actionType: "mute", severity: 3 }), NaN).valid).toBe(false);
    expect(
      validateAction(input({ actionType: "mute", severity: 3 }), Infinity).valid,
    ).toBe(false);
  });

  it("accepts a duration at exactly the maximum", () => {
    expect(
      validateAction(input({ actionType: "mute", severity: 3 }), MAX_DURATION_HOURS).valid,
    ).toBe(true);
  });

  it("refuses one hour over the maximum", () => {
    expect(
      validateAction(input({ actionType: "mute", severity: 3 }), MAX_DURATION_HOURS + 1).valid,
    ).toBe(false);
  });

  // Bans are permanent; the server rejects one that carries a duration, so a
  // leftover value in the form field must not make the action invalid.
  it("ignores a duration on action types that do not take one", () => {
    expect(validateAction(input({ actionType: "ban", severity: 5 }), 24).valid).toBe(true);
    expect(validateAction(input(), 0).valid).toBe(true);
  });

  it("returns a reason for every rejection, so the dialog can explain itself", () => {
    const result = validateAction(input({ actionType: "" }));
    expect(result.error).toBeTruthy();
  });

  it.each([
    ["a missing reason", { actionType: "warn", severity: 1 }],
    ["nothing at all", null],
  ])("refuses %s rather than throwing", (_label, malformed) => {
    expect(validateAction(malformed as unknown as ActionInput).valid).toBe(false);
  });
});

/**
 * validateActionRequest in internal/service/moderation_action.go refuses an
 * action whose moderator and target are the same person. The dialog knew only
 * the target, so it happily submitted one and took a guaranteed 400 for it.
 */
describe("validateAction on yourself", () => {
  const onSelf = (overrides: Partial<ActionInput> = {}) =>
    validateAction(input({ targetUserId: MODERATOR, ...overrides }));

  it("refuses an action a moderator aims at themselves", () => {
    const result = onSelf();
    expect(result.valid).toBe(false);
    expect(result.error).toBe("You cannot moderate yourself.");
  });

  it("allows the same action against somebody else", () => {
    expect(validateAction(input()).valid).toBe(true);
  });

  // The form is not what is wrong. Reporting a severity or reason problem first
  // would send the moderator off to fix fields that were never the reason the
  // action could not go through.
  it.each([
    ["a severity the action type does not allow", { severity: 5 }],
    ["an empty reason", { reason: "" }],
    ["no action type at all", { actionType: "" }],
  ])("says so ahead of %s", (_label, overrides) => {
    expect(onSelf(overrides).error).toBe("You cannot moderate yourself.");
  });

  // Two empty ids are what a viewer whose identity has not loaded looks like.
  // Reading that as self-moderation would block every action on the page.
  it("does not read an unknown viewer as the target", () => {
    expect(validateAction(input({ moderatorId: "", targetUserId: "" })).valid).toBe(true);
  });

  it("does not read a missing moderator as the target", () => {
    const missing = { ...input(), moderatorId: undefined } as unknown as ActionInput;
    expect(validateAction(missing).valid).toBe(true);
  });
});

describe("buildActionRequest", () => {
  it("converts the duration from hours to seconds", () => {
    const req = buildActionRequest(input({ actionType: "mute", severity: 3 }), 24);
    expect(req.duration_seconds).toBe(86_400);
  });

  it("rounds a fractional hour to whole seconds", () => {
    const req = buildActionRequest(input({ actionType: "mute", severity: 3 }), 1.5);
    expect(req.duration_seconds).toBe(5400);
  });

  // The server rejects either of these carrying a duration outright — neither
  // a warning nor a ban ends — so a value left in the form field must not reach
  // it.
  it.each(["warn", "ban"])("omits the duration for a %s", (actionType) => {
    const req = buildActionRequest(input({ actionType, severity: 1 }), 24);
    expect(req.duration_seconds).toBeUndefined();
  });

  it("trims the reason the way the server will", () => {
    const req = buildActionRequest(input({ reason: "  spam  " }));
    expect(req.reason).toBe("spam");
  });

  it("sends an empty reason rather than throwing when one is missing", () => {
    const req = buildActionRequest({
      actionType: "warn",
      severity: 1,
    } as unknown as ActionInput);
    expect(req.reason).toBe("");
  });

  it("carries the target and the action through unchanged", () => {
    const req = buildActionRequest(input({ actionType: "ban", severity: 5 }));
    expect(req).toMatchObject({
      target_user_id: "target-1",
      action_type: "ban",
      severity: 5,
    });
  });
});

describe("validateRemovalReason", () => {
  it("accepts an ordinary reason", () => {
    expect(validateRemovalReason("Harassment of another member")).toEqual({ valid: true });
  });

  // The reason is the only record of why a moderator overrode a member's
  // speech, so unlike an author's own deletion it cannot be blank.
  it.each(["", "   ", "\t\n "])("rejects the blank reason %o", (reason) => {
    expect(validateRemovalReason(reason).valid).toBe(false);
  });

  it("accepts a reason of exactly the maximum length", () => {
    expect(validateRemovalReason("a".repeat(MAX_REMOVAL_REASON_LENGTH)).valid).toBe(true);
  });

  it("rejects a reason one character over the maximum", () => {
    expect(validateRemovalReason("a".repeat(MAX_REMOVAL_REASON_LENGTH + 1)).valid).toBe(false);
  });

  // validateRemovalReason in internal/service/post.go measures len(reason),
  // which is bytes. Measuring JS string length instead would wave through a
  // reason of multi-byte characters that the server then rejects with a 400 —
  // exactly the round trip this check exists to prevent.
  it("measures the limit in bytes, as the server does", () => {
    const reason = "é".repeat(501); // 501 characters, 1002 bytes
    expect(reason.length).toBeLessThan(MAX_REMOVAL_REASON_LENGTH);
    expect(validateRemovalReason(reason).valid).toBe(false);
  });

  it("measures the trimmed reason, so padding cannot push it over", () => {
    expect(validateRemovalReason(`  ${"a".repeat(MAX_REMOVAL_REASON_LENGTH)}  `).valid).toBe(true);
  });
});

describe("canRemovePost", () => {
  const visible = { id: "p1", status: "visible" } as Post;

  it("offers removal for a visible post", () => {
    expect(canRemovePost(visible)).toBe(true);
  });

  // Mirrors canRemoveByModerator in internal/service/post.go: the server
  // refuses to re-remove, so offering the control would guarantee a 400.
  it.each(["removed_by_author", "removed_by_mod"])("hides the control for a %s post", (status) => {
    expect(canRemovePost({ ...visible, status } as Post)).toBe(false);
  });

  // The card renders before its post has loaded, and renders on error too.
  it("hides the control while the post has not loaded", () => {
    expect(canRemovePost(null)).toBe(false);
  });
});

describe("reportResolutionOutcome", () => {
  // The bug this exists to close: taking action removed the report from local
  // state and never told the server, so it came back `pending` on reload and
  // moderators re-reviewed work they had already done.
  it("resolves the report when the PATCH succeeded", () => {
    expect(reportResolutionOutcome(null)).toEqual({ resolved: true });
  });

  // Another moderator got there first. The report is gone either way, so it
  // should still leave the queue — this mirrors ReportCard's dismiss handler.
  it("treats a 404 as already resolved by someone else", () => {
    expect(reportResolutionOutcome({ status: 404, error: "not found" })).toEqual({
      resolved: true,
    });
  });

  // The action or removal already succeeded, so the UI must not claim the
  // report was handled when the server still has it pending. Leaving it in the
  // queue is what makes the divergence visible.
  it.each([
    [403, "forbidden"],
    [500, "internal error"],
    [400, "validation: status must be 'reviewed' or 'dismissed'"],
  ])("leaves the report in the queue on a %d", (status, message) => {
    const outcome = reportResolutionOutcome({ status, error: message });
    expect(outcome.resolved).toBe(false);
    expect(outcome.error).toBeTruthy();
  });

  it("surfaces the server's own wording", () => {
    const outcome = reportResolutionOutcome({ status: 500, error: "database is down" });
    expect(outcome.error).toContain("database is down");
  });

  // The action succeeded even though the follow-up failed, so the message must
  // not read as though nothing happened.
  it("says the action succeeded when only the follow-up failed", () => {
    const outcome = reportResolutionOutcome({ status: 500, error: "boom" });
    expect(outcome.error?.toLowerCase()).toContain("still");
  });

  it("copes with a rejection that carries no message", () => {
    const outcome = reportResolutionOutcome({ status: 0 } as ApiError);
    expect(outcome.resolved).toBe(false);
    expect(outcome.error).toBeTruthy();
  });
});

describe("activeMuteExpiry", () => {
  const now = new Date("2026-08-11T12:00:00Z");

  it("reports the expiry of a mute that is still running", () => {
    const expiry = activeMuteExpiry({ muted_until: "2026-08-12T12:00:00Z" }, now);
    expect(expiry?.toISOString()).toBe("2026-08-12T12:00:00.000Z");
  });

  it("reports no mute when the field is absent, which is how the server says not muted", () => {
    expect(activeMuteExpiry({}, now)).toBeNull();
  });

  it("reports no mute before the status has loaded", () => {
    expect(activeMuteExpiry(null, now)).toBeNull();
    expect(activeMuteExpiry(undefined, now)).toBeNull();
  });

  it("reports no mute for an expiry that has passed while the page was open", () => {
    expect(activeMuteExpiry({ muted_until: "2026-08-11T11:59:00Z" }, now)).toBeNull();
  });

  it("reports no mute for a timestamp it cannot parse, rather than an Invalid Date", () => {
    expect(activeMuteExpiry({ muted_until: "not a date" }, now)).toBeNull();
  });
});

describe("liftMuteBlockReason", () => {
  it("allows a moderator to lift someone else's mute", () => {
    expect(liftMuteBlockReason("mod-1", "target-1")).toBeNull();
  });

  it("refuses a moderator lifting their own mute, which the server rejects outright", () => {
    expect(liftMuteBlockReason("mod-1", "mod-1")).toBe("You cannot moderate yourself.");
  });

  it("allows the action while the viewer's identity is still unknown", () => {
    // Two empty strings are unknown, not self — the same rule validateAction
    // applies, so a viewer whose identity has not loaded is not told they are
    // moderating themselves.
    expect(liftMuteBlockReason("", "")).toBeNull();
    expect(liftMuteBlockReason("", "target-1")).toBeNull();
  });
});

describe("ownMuteNotice", () => {
  const now = new Date("2026-03-01T14:00:00Z");

  it("reports an active mute on the member's own profile", () => {
    const notice = ownMuteNotice(
      { muted_until: "2026-03-02T14:00:00Z" } as User,
      now,
    );
    expect(notice.mutedUntil).toEqual(new Date("2026-03-02T14:00:00Z"));
  });

  it("treats an expired mute as no mute, needing no clock of the caller's own", () => {
    // The server already omits an expired muted_until, but a page left open
    // outlives the response that loaded it.
    const notice = ownMuteNotice(
      { muted_until: "2026-03-01T13:00:00Z" } as User,
      now,
    );
    expect(notice.mutedUntil).toBeNull();
  });

  it("surfaces the most recent lift so a released member learns it happened", () => {
    const notice = ownMuteNotice(
      {
        mute_lifts: [
          { lifted_at: "2026-03-01T12:00:00Z", previous_muted_until: "2026-03-02T14:00:00Z" },
          { lifted_at: "2026-02-01T12:00:00Z" },
        ],
      } as User,
      now,
    );
    expect(notice.latestLift?.lifted_at).toBe("2026-03-01T12:00:00Z");
  });

  it("reports nothing for a member who has never been muted", () => {
    const notice = ownMuteNotice({} as User, now);
    expect(notice.mutedUntil).toBeNull();
    expect(notice.latestLift).toBeNull();
    expect(notice.hasAnything).toBe(false);
  });

  it("keeps showing the lift while a later mute is in force, since they are different facts", () => {
    // Being muted again does not undo having been released from an earlier one,
    // and collapsing the two would hide half the member's record.
    const notice = ownMuteNotice(
      {
        muted_until: "2026-03-02T14:00:00Z",
        mute_lifts: [{ lifted_at: "2026-02-01T12:00:00Z" }],
      } as User,
      now,
    );
    expect(notice.mutedUntil).not.toBeNull();
    expect(notice.latestLift).not.toBeNull();
  });

  it("ignores an unparseable timestamp rather than rendering Invalid Date", () => {
    const notice = ownMuteNotice({ muted_until: "not a date" } as User, now);
    expect(notice.mutedUntil).toBeNull();
  });

  it("reads a profile that is not the caller's own as having nothing to report", () => {
    // Another member's profile carries neither field, and must never be made to
    // look like it does.
    expect(ownMuteNotice(null, now).hasAnything).toBe(false);
  });
});

describe("describeHistorySubject", () => {
  const SUBJECT = "0193a7b2-5f3e-7000-8000-000000000000";

  function entry(targetName?: string): ActionHistoryEntry {
    return {
      action: {
        id: `action-${targetName ?? "anon"}`,
        target_user_id: SUBJECT,
        target_display_name: targetName,
        moderator_id: MODERATOR,
        action: "warn",
        severity: 1,
        reason: "Minor issue",
        duration: null,
        created_at: "2026-03-01T12:00:00Z",
        expires_at: null,
      },
      penalties: [],
    };
  }

  // The page loads a list of actions and nothing else, so the name has to come
  // from the actions — every one of them is addressed to this member.
  it("names the subject from the actions taken against them", () => {
    expect(describeHistorySubject([entry("Ada Lovelace")], SUBJECT)).toContain("Ada Lovelace");
  });

  // Two members can share a display name, and a moderator about to act needs to
  // know which one they are reading.
  it("keeps the short id beside the name", () => {
    expect(describeHistorySubject([entry("Ada Lovelace")], SUBJECT)).toBe(
      "Ada Lovelace · 0193a7b2...",
    );
  });

  it("falls back to the id alone, unrepeated, when no action carries a name", () => {
    expect(describeHistorySubject([entry(), entry()], SUBJECT)).toBe("0193a7b2...");
  });

  it("takes the name from a later action when the first carries none", () => {
    expect(describeHistorySubject([entry(), entry("Ada Lovelace")], SUBJECT)).toContain(
      "Ada Lovelace",
    );
  });

  it.each([
    ["an empty history, which is the first thing the page renders", []],
    ["a history that has not loaded", undefined],
    ["a null history", null],
  ])("still identifies the subject with %s", (_label, entries) => {
    expect(describeHistorySubject(entries, SUBJECT)).toBe("0193a7b2...");
  });

  it("ignores a whitespace-only name rather than rendering a blank subject", () => {
    expect(describeHistorySubject([entry("   ")], SUBJECT)).toBe("0193a7b2...");
  });
});
