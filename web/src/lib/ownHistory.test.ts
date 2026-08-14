import { describe, expect, it } from "vitest";
import type { OwnModerationEntry } from "../api/types";
import { describeOwnAction, OWN_HISTORY_EMPTY } from "./ownHistory";

/**
 * These pin the wording a member reads about their own moderation. The tone is
 * as much the requirement as the facts: the person on the receiving end of a
 * warning should learn what happened and when it stops counting, without being
 * handed a severity badge or told off a second time.
 */

const NOW = new Date("2026-03-01T12:00:00Z");

function entry(overrides: Partial<OwnModerationEntry> = {}): OwnModerationEntry {
  return {
    id: "act-1",
    action: "warn",
    severity: 1,
    reason: "posting the same thing repeatedly",
    created_at: "2026-03-01T12:00:00Z",
    ...overrides,
  };
}

describe("describeOwnAction", () => {
  it("names a warning by its severity, in words", () => {
    expect(describeOwnAction(entry({ severity: 1 }), NOW).headline).toBe("You were warned — minor");
    expect(describeOwnAction(entry({ severity: 2 }), NOW).headline).toBe(
      "You were warned — moderate",
    );
  });

  it("does not repeat the severity for actions that carry only one", () => {
    // A mute is always severity 3 and a suspension always 4, so naming the
    // level would tell the member nothing they did not just read.
    expect(describeOwnAction(entry({ action: "mute", severity: 3 }), NOW).headline).toBe(
      "You were muted",
    );
    expect(describeOwnAction(entry({ action: "suspend", severity: 4 }), NOW).headline).toBe(
      "You were suspended",
    );
    expect(describeOwnAction(entry({ action: "ban", severity: 5 }), NOW).headline).toBe(
      "You were banned",
    );
  });

  it("still shows a record whose action type this build does not know", () => {
    // A member must not be kept from a real moderation decision because the
    // frontend is a version behind the server.
    const summary = describeOwnAction(entry({ action: "shadowban", severity: 9 }), NOW);
    expect(summary.headline).toBe("A moderator acted on your account");
    expect(summary.reason).toBe("posting the same thing repeatedly");
  });

  it("passes the moderator's reason through exactly as written", () => {
    const reason = "Sharing a neighbour's address in a comment.";
    expect(describeOwnAction(entry({ reason }), NOW).reason).toBe(reason);
  });

  it("says when a restriction still in force will end", () => {
    const summary = describeOwnAction(
      entry({ action: "mute", severity: 3, expires_at: "2026-03-04T12:00:00Z" }),
      NOW,
    );
    expect(summary.restriction).toMatch(/^This ends on /);
  });

  it("switches to the past tense once the restriction has run out", () => {
    // A member reading an old mute must not be left believing they still
    // cannot post.
    const summary = describeOwnAction(
      entry({ action: "mute", severity: 3, expires_at: "2026-02-01T12:00:00Z" }),
      NOW,
    );
    expect(summary.restriction).toMatch(/^This ended on /);
  });

  it("has no restriction line for an action that does not expire", () => {
    expect(describeOwnAction(entry({ action: "warn" }), NOW).restriction).toBeNull();
    expect(describeOwnAction(entry({ action: "ban", severity: 5 }), NOW).restriction).toBeNull();
  });

  it("shows nothing rather than 'Invalid Date' for an unparseable expiry", () => {
    expect(describeOwnAction(entry({ expires_at: "not a date" }), NOW).restriction).toBeNull();
  });

  it("says what the action cost and when the cost fades", () => {
    const summary = describeOwnAction(
      entry({ penalty: { amount: 5, decays_at: "2026-05-30T12:00:00Z" } }),
      NOW,
    );
    expect(summary.cost).toBe("This cost 5 trust points; fully faded by May 2026.");
  });

  it("says a ban's penalty does not fade", () => {
    const summary = describeOwnAction(
      entry({ action: "ban", severity: 5, penalty: { amount: 100 } }),
      NOW,
    );
    expect(summary.cost).toBe("This cost 100 trust points, and they do not fade.");
  });

  it("does not claim a decaying penalty is permanent when the date is missing", () => {
    // A missing decays_at and a permanent penalty look identical on the wire.
    // Severity 1 decays over 90 days, so the honest answer is to state the cost
    // and say nothing about fading.
    const summary = describeOwnAction(entry({ severity: 1, penalty: { amount: 5 } }), NOW);
    expect(summary.cost).toBe("This cost 5 trust points.");
  });

  it("has no cost line when no penalty was recorded", () => {
    expect(describeOwnAction(entry(), NOW).cost).toBeNull();
  });

  it("rounds a fractional penalty to something readable", () => {
    const summary = describeOwnAction(
      entry({ penalty: { amount: 28.000000000000004, decays_at: "2027-03-01T12:00:00Z" } }),
      NOW,
    );
    expect(summary.cost).toContain("28 trust points");
  });

  it("never mentions a moderator", () => {
    // The server sends none. This is the frontend half of the same promise:
    // nothing here may invent an actor to attribute the decision to.
    const summary = describeOwnAction(
      entry({
        action: "suspend",
        severity: 4,
        expires_at: "2026-03-08T12:00:00Z",
        penalty: { amount: 40, decays_at: "2027-03-01T12:00:00Z" },
      }),
      NOW,
    );
    const rendered = Object.values(summary).join(" ");
    expect(rendered.toLowerCase()).not.toContain("moderator");
    // No attribution of any kind: not "by Mallory", not "a moderator decided".
    // ("faded by March 2027" is about the penalty, not about a person, so the
    // assertion is on the shape rather than on the word "by".)
    expect(Object.keys(summary).sort()).toEqual([
      "cost",
      "headline",
      "reason",
      "restriction",
      "when",
    ]);
  });
});

describe("OWN_HISTORY_EMPTY", () => {
  it("treats a clean record as normal rather than as an absence", () => {
    expect(OWN_HISTORY_EMPTY).toBe("Nothing here — and that's how it stays for most people.");
  });
});
