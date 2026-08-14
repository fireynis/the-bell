import { describe, expect, it } from "vitest";
import {
  REACTION_TYPES,
  reactionEmoji,
  reactionLabel,
  revertReaction,
  toggleReaction,
  type ReactionState,
} from "./reactions";

describe("reactionEmoji", () => {
  it.each([
    ["bell", "🔔"],
    ["heart", "❤️"],
    ["celebrate", "🎉"],
  ])("maps %s", (type, emoji) => {
    expect(reactionEmoji(type)).toBe(emoji);
  });

  it("falls back for an unknown type rather than rendering undefined", () => {
    expect(reactionEmoji("brand-new-reaction")).toBe("👍");
  });

  it("falls back for an empty type", () => {
    expect(reactionEmoji("")).toBe("👍");
  });

  // The buttons are rendered from REACTION_TYPES, so a type in that list with
  // no emoji would ship a row of identical fallback glyphs.
  it("has a glyph for every type the UI offers", () => {
    for (const type of REACTION_TYPES) {
      expect(reactionEmoji(type)).not.toBe("👍");
    }
  });
});

describe("toggleReaction", () => {
  it("adds the reader's reaction when they have not reacted", () => {
    expect(toggleReaction({ count: 2, active: false })).toEqual({ count: 3, active: true });
  });

  it("removes it when they have", () => {
    expect(toggleReaction({ count: 3, active: true })).toEqual({ count: 2, active: false });
  });

  it("decrements exactly once for an already-active reaction", () => {
    expect(toggleReaction({ count: 5, active: true }).count).toBe(4);
  });

  // A stale count of zero on an active reaction would otherwise render "-1".
  it("never produces a negative count", () => {
    expect(toggleReaction({ count: 0, active: true })).toEqual({ count: 0, active: false });
  });

  it("does not mutate the state it was given", () => {
    const state: ReactionState = { count: 1, active: false };
    toggleReaction(state);
    expect(state).toEqual({ count: 1, active: false });
  });
});

describe("revertReaction", () => {
  it.each([
    [{ count: 2, active: false }],
    [{ count: 3, active: true }],
    [{ count: 0, active: false }],
  ])("round-trips %o back to itself", (before: ReactionState) => {
    expect(revertReaction(toggleReaction(before), before)).toEqual(before);
  });

  // A second revert must not subtract again — a failed request can be reverted
  // by both the catch block and a later resync.
  it("leaves a state that is already reverted alone", () => {
    const before: ReactionState = { count: 4, active: true };
    const once = revertReaction(toggleReaction(before), before);
    expect(revertReaction(once, before)).toEqual(before);
  });

  it("never restores a negative count", () => {
    const got = revertReaction({ count: 0, active: true }, { count: -1, active: false });
    expect(got.count).toBe(0);
  });
});

/**
 * The button shows an emoji and a bare number, both aria-hidden. Without this
 * a screen reader announced "🔔 3" — neither what the control is nor what the
 * number counts.
 */
describe("reactionLabel", () => {
  it("names the reaction and counts what the number is counting", () => {
    expect(reactionLabel("bell", 3)).toBe("Bell, 3 reactions");
  });

  it("does not say '1 reactions'", () => {
    expect(reactionLabel("heart", 1)).toBe("Heart, 1 reaction");
  });

  // The count is hidden at zero, so the name is the only thing said.
  it("says so when nobody has reacted", () => {
    expect(reactionLabel("celebrate", 0)).toBe("Celebrate, no reactions");
  });

  it.each(REACTION_TYPES)("names %s rather than reading the raw type", (type) => {
    expect(reactionLabel(type, 1).startsWith(type)).toBe(false);
  });

  // Same rule as reactionEmoji: the server can start sending a type this build
  // has never heard of, and it must not leak into what a reader hears.
  it("falls back to a generic name for an unknown type", () => {
    expect(reactionLabel("applaud", 2)).toBe("Reaction, 2 reactions");
  });
});
