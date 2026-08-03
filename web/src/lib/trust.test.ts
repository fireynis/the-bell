import { describe, expect, it } from "vitest";
import type { User } from "../api/types";
import {
  POSTING_THRESHOLD,
  VOUCHING_THRESHOLD,
  canModerate,
  canPost,
  canVouch,
  clampTrustScore,
  isCouncil,
  trustTier,
} from "./trust";

function user(overrides: Partial<User> = {}): User {
  return {
    id: "u1",
    display_name: "Resident",
    bio: "",
    avatar_url: "",
    trust_score: 50,
    role: "member",
    is_active: true,
    joined_at: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

describe("clampTrustScore", () => {
  it.each([
    [50, 50],
    [0, 0],
    [100, 100],
    [-10, 0],
    [140, 100],
  ])("clamps %d to %d", (input, want) => {
    expect(clampTrustScore(input)).toBe(want);
  });

  // A NaN width would render a broken bar rather than an empty one.
  it("maps NaN to zero and clamps infinities to the bounds", () => {
    expect(clampTrustScore(NaN)).toBe(0);
    expect(clampTrustScore(Infinity)).toBe(100);
    expect(clampTrustScore(-Infinity)).toBe(0);
  });
});

describe("trustTier", () => {
  it.each([
    [100, "high"],
    [VOUCHING_THRESHOLD, "high"],
    [VOUCHING_THRESHOLD - 0.1, "medium"],
    [POSTING_THRESHOLD, "medium"],
    [POSTING_THRESHOLD - 0.1, "low"],
    [0, "low"],
  ])("scores %d as %s", (score, want) => {
    expect(trustTier(score)).toBe(want);
  });

  // The tiers exist to signal capability, so their boundaries must line up with
  // the thresholds that actually gate posting and vouching.
  it("aligns its boundaries with the action thresholds", () => {
    expect(trustTier(VOUCHING_THRESHOLD)).toBe("high");
    expect(canVouch(user({ trust_score: VOUCHING_THRESHOLD }))).toBe(true);

    expect(trustTier(POSTING_THRESHOLD)).toBe("medium");
    expect(canPost(user({ trust_score: POSTING_THRESHOLD }))).toBe(true);
  });
});

describe("canPost", () => {
  it("allows an active member at the threshold", () => {
    expect(canPost(user({ trust_score: POSTING_THRESHOLD }))).toBe(true);
  });

  it("refuses just below the threshold", () => {
    expect(canPost(user({ trust_score: POSTING_THRESHOLD - 0.1 }))).toBe(false);
  });

  it("refuses an inactive user however high their score", () => {
    expect(canPost(user({ trust_score: 100, is_active: false }))).toBe(false);
  });

  it.each(["pending", "banned"])("refuses a %s user however high their score", (role) => {
    expect(canPost(user({ trust_score: 100, role }))).toBe(false);
  });

  it("allows moderators and council", () => {
    expect(canPost(user({ role: "moderator", trust_score: 80 }))).toBe(true);
    expect(canPost(user({ role: "council", trust_score: 80 }))).toBe(true);
  });

  it("refuses when there is no signed-in user", () => {
    expect(canPost(null)).toBe(false);
  });
});

describe("canVouch", () => {
  it("allows an active member at the threshold", () => {
    expect(canVouch(user({ trust_score: VOUCHING_THRESHOLD }))).toBe(true);
  });

  it("refuses just below the threshold", () => {
    expect(canVouch(user({ trust_score: VOUCHING_THRESHOLD - 0.1 }))).toBe(false);
  });

  // Vouching is the higher bar: being able to post must not imply it.
  it("is stricter than posting", () => {
    const between = user({ trust_score: (POSTING_THRESHOLD + VOUCHING_THRESHOLD) / 2 });
    expect(canPost(between)).toBe(true);
    expect(canVouch(between)).toBe(false);
  });

  it.each(["pending", "banned"])("refuses a %s user", (role) => {
    expect(canVouch(user({ trust_score: 100, role }))).toBe(false);
  });

  it("refuses when there is no signed-in user", () => {
    expect(canVouch(null)).toBe(false);
  });
});

describe("canModerate", () => {
  it.each([
    ["moderator", true],
    ["council", true],
    ["member", false],
    ["pending", false],
    ["banned", false],
  ])("role %s -> %s", (role, want) => {
    expect(canModerate(user({ role }))).toBe(want);
  });

  // Moderation is a role, not a score: a low-trust moderator still moderates
  // until the role checker demotes them.
  it("ignores the trust score", () => {
    expect(canModerate(user({ role: "moderator", trust_score: 0 }))).toBe(true);
  });

  it("refuses a deactivated moderator", () => {
    expect(canModerate(user({ role: "moderator", is_active: false }))).toBe(false);
  });

  it("refuses when there is no signed-in user", () => {
    expect(canModerate(null)).toBe(false);
  });
});

describe("isCouncil", () => {
  it("recognises only active council members", () => {
    expect(isCouncil(user({ role: "council" }))).toBe(true);
    expect(isCouncil(user({ role: "council", is_active: false }))).toBe(false);
    expect(isCouncil(user({ role: "moderator" }))).toBe(false);
    expect(isCouncil(null)).toBe(false);
  });
});
