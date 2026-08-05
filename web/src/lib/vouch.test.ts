import { describe, expect, it } from "vitest";
import type { User, Vouch } from "../api/types";
import { VOUCHING_THRESHOLD } from "./trust";
import {
  DAILY_VOUCH_LIMIT,
  REVOKE_PENALTY,
  REVOKE_PENALTY_DAYS,
  REVOKE_PENALTY_ENFORCED,
  canRevokeVouch,
  countVouchesToday,
  describeRevokeCost,
  findActiveVouch,
  vouchBlockReason,
} from "./vouch";

/** Midday, so "earlier today" and "yesterday" are both expressible. */
const NOW = new Date(2026, 2, 1, 12, 0, 0).getTime();

const VIEWER = "viewer-1";
const TARGET = "target-1";

function user(overrides: Partial<User> = {}): User {
  return {
    id: VIEWER,
    display_name: "Resident",
    bio: "",
    avatar_url: "",
    trust_score: VOUCHING_THRESHOLD,
    role: "member",
    is_active: true,
    joined_at: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

function vouch(overrides: Partial<Vouch> = {}): Vouch {
  return {
    id: "v1",
    voucher_id: VIEWER,
    vouchee_id: TARGET,
    status: "active",
    created_at: new Date(NOW).toISOString(),
    ...overrides,
  };
}

/** hoursAgo returns an ISO timestamp the given number of hours before NOW. */
function hoursAgo(hours: number): string {
  return new Date(NOW - hours * 3_600_000).toISOString();
}

describe("findActiveVouch", () => {
  it("finds the viewer's vouch for the target", () => {
    expect(findActiveVouch([vouch()], VIEWER, TARGET)?.id).toBe("v1");
  });

  it("ignores a vouch from someone else for the same person", () => {
    expect(findActiveVouch([vouch({ voucher_id: "other" })], VIEWER, TARGET)).toBeNull();
  });

  it("ignores the viewer's vouch for a different person", () => {
    expect(findActiveVouch([vouch({ vouchee_id: "other" })], VIEWER, TARGET)).toBeNull();
  });

  // The pair is unique in the database and its status toggles, so a revoked row
  // means the viewer may vouch again, not that they already have.
  it("ignores a revoked vouch, which may be given again", () => {
    expect(findActiveVouch([vouch({ status: "revoked" })], VIEWER, TARGET)).toBeNull();
  });

  it.each([
    ["an empty list", []],
    ["a missing list", undefined],
    ["a null list", null],
  ])("returns null for %s", (_label, input) => {
    expect(findActiveVouch(input, VIEWER, TARGET)).toBeNull();
  });

  it("skips malformed entries rather than throwing", () => {
    const got = findActiveVouch([null as unknown as Vouch, vouch()], VIEWER, TARGET);
    expect(got?.id).toBe("v1");
  });
});

describe("countVouchesToday", () => {
  it("counts a vouch given earlier today", () => {
    expect(countVouchesToday([vouch({ created_at: hoursAgo(2) })], NOW)).toBe(1);
  });

  it("counts a vouch given at the stroke of midnight", () => {
    const midnight = new Date(2026, 2, 1, 0, 0, 0).toISOString();
    expect(countVouchesToday([vouch({ created_at: midnight })], NOW)).toBe(1);
  });

  it("does not count one from a minute before midnight", () => {
    const lastNight = new Date(2026, 1, 28, 23, 59, 0).toISOString();
    expect(countVouchesToday([vouch({ created_at: lastNight })], NOW)).toBe(0);
  });

  it("does not count yesterday's vouches", () => {
    expect(countVouchesToday([vouch({ created_at: hoursAgo(24) })], NOW)).toBe(0);
  });

  it("counts several from today", () => {
    const given = [
      vouch({ id: "a", created_at: hoursAgo(1) }),
      vouch({ id: "b", created_at: hoursAgo(5) }),
      vouch({ id: "c", created_at: hoursAgo(30) }),
    ];
    expect(countVouchesToday(given, NOW)).toBe(2);
  });

  it("ignores an unparseable timestamp rather than counting it", () => {
    expect(countVouchesToday([vouch({ created_at: "not a date" })], NOW)).toBe(0);
  });

  it("skips malformed entries rather than throwing", () => {
    const given = [null as unknown as Vouch, vouch({ created_at: hoursAgo(1) })];
    expect(countVouchesToday(given, NOW)).toBe(1);
  });

  // Mild clock skew can date a vouch a moment into the future, and counting it
  // is the safe direction — the server counts it too. A timestamp days ahead is
  // not skew, and must not consume today's allowance.
  it("tolerates slight clock skew but ignores a far-future timestamp", () => {
    const skewed = new Date(NOW + 60_000).toISOString();
    expect(countVouchesToday([vouch({ created_at: skewed })], NOW)).toBe(1);

    const nextWeek = new Date(NOW + 7 * 86_400_000).toISOString();
    expect(countVouchesToday([vouch({ created_at: nextWeek })], NOW)).toBe(0);
  });

  it.each([
    ["a missing list", undefined],
    ["a null list", null],
  ])("reports zero for %s", (_label, input) => {
    expect(countVouchesToday(input, NOW)).toBe(0);
  });
});

describe("vouchBlockReason", () => {
  it("permits an eligible member vouching for someone else", () => {
    expect(vouchBlockReason(user(), TARGET, [], NOW)).toBeNull();
  });

  it("refuses when nobody is signed in", () => {
    expect(vouchBlockReason(null, TARGET, [], NOW)).toContain("signed in");
  });

  // More specific than "your trust is too low": a low-trust member on their own
  // profile is looking at the wrong person, not short of trust.
  it("refuses self-vouching before it mentions trust", () => {
    const viewer = user({ trust_score: 0 });
    expect(vouchBlockReason(viewer, viewer.id, [], NOW)).toBe("You cannot vouch for yourself.");
  });

  it("refuses self-vouching even for a fully eligible member", () => {
    const viewer = user({ trust_score: 100 });
    expect(vouchBlockReason(viewer, viewer.id, [], NOW)).toContain("yourself");
  });

  it("refuses just below the vouching threshold", () => {
    const viewer = user({ trust_score: VOUCHING_THRESHOLD - 0.1 });
    expect(vouchBlockReason(viewer, TARGET, [], NOW)).toContain(String(VOUCHING_THRESHOLD));
  });

  it("permits at exactly the threshold", () => {
    const viewer = user({ trust_score: VOUCHING_THRESHOLD });
    expect(vouchBlockReason(viewer, TARGET, [], NOW)).toBeNull();
  });

  it("refuses a deactivated member however high their score", () => {
    const viewer = user({ trust_score: 100, is_active: false });
    expect(vouchBlockReason(viewer, TARGET, [], NOW)).toContain("suspended");
  });

  it.each(["pending", "banned"])("refuses a %s member", (role) => {
    expect(vouchBlockReason(user({ trust_score: 100, role }), TARGET, [], NOW)).toBeTruthy();
  });

  it("refuses vouching twice for the same member", () => {
    expect(vouchBlockReason(user(), TARGET, [vouch()], NOW)).toContain("already vouched");
  });

  it("permits vouching again after a revocation", () => {
    expect(vouchBlockReason(user(), TARGET, [vouch({ status: "revoked" })], NOW)).toBeNull();
  });

  it("permits the last vouch of the day", () => {
    const given = Array.from({ length: DAILY_VOUCH_LIMIT - 1 }, (_, i) =>
      vouch({ id: `v${i}`, vouchee_id: `other-${i}`, created_at: hoursAgo(1) }),
    );
    expect(vouchBlockReason(user(), TARGET, given, NOW)).toBeNull();
  });

  it("refuses one past the daily limit", () => {
    const given = Array.from({ length: DAILY_VOUCH_LIMIT }, (_, i) =>
      vouch({ id: `v${i}`, vouchee_id: `other-${i}`, created_at: hoursAgo(1) }),
    );
    expect(vouchBlockReason(user(), TARGET, given, NOW)).toContain(String(DAILY_VOUCH_LIMIT));
  });

  it("forgets yesterday's limit", () => {
    const given = Array.from({ length: DAILY_VOUCH_LIMIT }, (_, i) =>
      vouch({ id: `v${i}`, vouchee_id: `other-${i}`, created_at: hoursAgo(25) }),
    );
    expect(vouchBlockReason(user(), TARGET, given, NOW)).toBeNull();
  });

  // The duplicate check is the more useful answer when both apply: "you already
  // vouched for them" tells the reader the action is pointless, where the limit
  // would wrongly suggest waiting until tomorrow would help.
  it("reports the duplicate before the daily limit when both apply", () => {
    const given = [
      vouch({ created_at: hoursAgo(1) }),
      vouch({ id: "b", vouchee_id: "other-1", created_at: hoursAgo(1) }),
      vouch({ id: "c", vouchee_id: "other-2", created_at: hoursAgo(1) }),
    ];
    expect(vouchBlockReason(user(), TARGET, given, NOW)).toContain("already vouched");
  });

  it("tolerates a missing vouch list", () => {
    expect(vouchBlockReason(user(), TARGET, undefined, NOW)).toBeNull();
  });
});

describe("canRevokeVouch", () => {
  it("lets the voucher take their own vouch back", () => {
    expect(canRevokeVouch(vouch(), user())).toBe(true);
  });

  it("refuses an unrelated member", () => {
    expect(canRevokeVouch(vouch({ voucher_id: "someone-else" }), user())).toBe(false);
  });

  it.each(["moderator", "council"])("lets a %s revoke anyone's vouch", (role) => {
    expect(canRevokeVouch(vouch({ voucher_id: "someone-else" }), user({ role }))).toBe(true);
  });

  it("refuses an already-revoked vouch, which cannot be revoked twice", () => {
    expect(canRevokeVouch(vouch({ status: "revoked" }), user())).toBe(false);
  });

  it("refuses a deactivated moderator", () => {
    const actor = user({ role: "moderator", is_active: false });
    expect(canRevokeVouch(vouch({ voucher_id: "someone-else" }), actor)).toBe(false);
  });

  it("refuses when there is no signed-in user", () => {
    expect(canRevokeVouch(vouch(), null)).toBe(false);
  });

  it("refuses when there is no vouch", () => {
    expect(canRevokeVouch(null, user())).toBe(false);
  });
});

describe("describeRevokeCost", () => {
  it("names the member whose endorsement is being withdrawn", () => {
    expect(describeRevokeCost("Ada Lovelace")).toContain("Ada Lovelace");
  });

  it.each([undefined, "", "   "])("falls back to a neutral phrase for %o", (name) => {
    expect(describeRevokeCost(name)).toContain("this member");
  });

  // Revoking must not read as an undo button.
  it("says the other member's score goes down", () => {
    expect(describeRevokeCost()).toContain("lowers their trust score");
  });

  it("warns that vouching again is rationed", () => {
    expect(describeRevokeCost()).toContain(String(DAILY_VOUCH_LIMIT));
  });

  // The server does not apply the design doc's revocation penalty yet, and
  // promising a cost that is never charged is a lie the UI must not tell.
  it("stays silent about a personal trust penalty while none is applied", () => {
    expect(describeRevokeCost("Ada", false)).not.toContain("costs you");
  });

  it("names the penalty once the server applies it", () => {
    const warning = describeRevokeCost("Ada", true);
    expect(warning).toContain(`costs you ${REVOKE_PENALTY} trust for ${REVOKE_PENALTY_DAYS} days`);
  });

  // Guards the default: the copy shown today must match what is actually
  // enforced today, whichever way the flag is set.
  it("defaults to whatever the server currently enforces", () => {
    expect(describeRevokeCost("Ada")).toBe(describeRevokeCost("Ada", REVOKE_PENALTY_ENFORCED));
  });
});
