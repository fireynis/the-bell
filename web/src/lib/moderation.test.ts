import { describe, expect, it } from "vitest";
import {
  ACTION_TYPES,
  MAX_ACTION_REASON_LENGTH,
  MAX_DURATION_HOURS,
  buildActionRequest,
  needsDuration,
  severitiesFor,
  validateAction,
  type ActionInput,
} from "./moderation";

function input(overrides: Partial<ActionInput> = {}): ActionInput {
  return { actionType: "warn", severity: 1, reason: "Repeated spam", ...overrides };
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

describe("buildActionRequest", () => {
  it("converts the duration from hours to seconds", () => {
    const req = buildActionRequest("target-1", input({ actionType: "mute", severity: 3 }), 24);
    expect(req.duration_seconds).toBe(86_400);
  });

  it("rounds a fractional hour to whole seconds", () => {
    const req = buildActionRequest("target-1", input({ actionType: "mute", severity: 3 }), 1.5);
    expect(req.duration_seconds).toBe(5400);
  });

  // The server rejects a ban that carries a duration outright, so a value left
  // in the form field must not reach it.
  it.each(["warn", "ban"])("omits the duration for a %s", (actionType) => {
    const req = buildActionRequest("target-1", input({ actionType, severity: 1 }), 24);
    expect(req.duration_seconds).toBeUndefined();
  });

  it("trims the reason the way the server will", () => {
    const req = buildActionRequest("target-1", input({ reason: "  spam  " }));
    expect(req.reason).toBe("spam");
  });

  it("sends an empty reason rather than throwing when one is missing", () => {
    const req = buildActionRequest("target-1", {
      actionType: "warn",
      severity: 1,
    } as unknown as ActionInput);
    expect(req.reason).toBe("");
  });

  it("carries the target and the action through unchanged", () => {
    const req = buildActionRequest("target-1", input({ actionType: "ban", severity: 5 }));
    expect(req).toMatchObject({
      target_user_id: "target-1",
      action_type: "ban",
      severity: 5,
    });
  });
});
