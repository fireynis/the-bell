import { describe, expect, it } from "vitest";
import type { ApiError, Invite, TownConfig, User } from "../api/types";
import {
  INVITE_CONSEQUENCE,
  INVITE_COOKIE_NAME,
  MAX_INVITE_NOTE_LENGTH,
  canInvite,
  canRevokeInvite,
  describeInviteCount,
  describeInviteExpiry,
  describeInviteStatus,
  inviteErrorMessage,
  invitedGreeting,
  registrationMode,
  remainingNoteChars,
  setInviteCookie,
  validateInviteEmail,
  validateInviteNote,
} from "./invite";
import { VOUCHING_THRESHOLD } from "./trust";
import { DAILY_VOUCH_LIMIT } from "./vouch";

/**
 * The invitation is the vouch, and every rule here follows from that: the gate
 * is the vouching gate, the budget is the vouching budget, and the sentence the
 * sender reads before they send says so. These pin that the client cannot come
 * to a different conclusion than the server about who may invite, and that an
 * invitation's state is described honestly once the response that carried it is
 * an hour old.
 */

function member(overrides: Partial<User> = {}): User {
  return {
    id: "u1",
    display_name: "Ada Lovelace",
    bio: "",
    avatar_url: "",
    trust_score: 75,
    role: "member",
    is_active: true,
    joined_at: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

function invite(overrides: Partial<Invite> = {}): Invite {
  return {
    id: "i1",
    email: "newcomer@example.com",
    note: "",
    status: "open",
    created_at: "2026-03-01T00:00:00Z",
    expires_at: "2026-03-15T00:00:00Z",
    ...overrides,
  };
}

const apiError = (status: number, error = ""): ApiError => ({ status, error });

/** 2026-03-01T00:00:00Z, the clock every expiry case below is measured against. */
const NOW = Date.parse("2026-03-01T00:00:00Z");
const days = (n: number) => new Date(NOW + n * 86_400_000).toISOString();

describe("canInvite", () => {
  it("lets a member who could vouch invite", () => {
    expect(canInvite(member({ trust_score: VOUCHING_THRESHOLD }))).toBe(true);
  });

  // The invitation creates the vouch, so anything that let this through would
  // be a way of vouching without meeting the vouching rule.
  it("refuses a member who is short of the vouching threshold", () => {
    expect(canInvite(member({ trust_score: VOUCHING_THRESHOLD - 1 }))).toBe(false);
  });

  // Day one of a town: nobody's score is near 60 and somebody has to be able to
  // let the first residents in.
  it("lets council invite whatever their score", () => {
    expect(canInvite(member({ role: "council", trust_score: 0 }))).toBe(true);
  });

  it("refuses a suspended council member", () => {
    expect(canInvite(member({ role: "council", trust_score: 90, is_active: false }))).toBe(false);
  });

  it.each(["pending", "banned"])("refuses a %s account however high its score", (role) => {
    expect(canInvite(member({ role, trust_score: 100 }))).toBe(false);
  });

  it("refuses nobody at all", () => {
    expect(canInvite(null)).toBe(false);
  });
});

describe("registrationMode", () => {
  it("reads invite mode off the town config", () => {
    expect(registrationMode({ registration_mode: "invite" })).toBe("invite");
  });

  it("reads open mode off the town config", () => {
    expect(registrationMode({ registration_mode: "open" })).toBe("open");
  });

  // A config read that did not land must not bolt the front door: the server
  // refuses an uninvited registration anyway, and guessing "invite" locks
  // everyone out of a town that never asked for invitations.
  it("falls back to open when the config says nothing", () => {
    expect(registrationMode({})).toBe("open");
    expect(registrationMode(null)).toBe("open");
  });

  it("falls back to open for a mode this build has never heard of", () => {
    expect(registrationMode({ registration_mode: "lottery" } as unknown as TownConfig)).toBe("open");
  });
});

describe("setInviteCookie", () => {
  it("writes the raw token where the Kratos proxy looks for it", () => {
    const doc = { cookie: "" };

    setInviteCookie("tok-abc123", doc);

    expect(doc.cookie).toContain(`${INVITE_COOKIE_NAME}=tok-abc123`);
    expect(doc.cookie).toContain("Path=/");
    expect(doc.cookie).toContain("SameSite=Lax");
    expect(doc.cookie).toContain("Max-Age=3600");
  });

  // The token arrives off a URL somebody was emailed; a stray `;` would
  // otherwise truncate the cookie into something the proxy cannot match.
  it("encodes a token containing characters a cookie cares about", () => {
    const doc = { cookie: "" };

    setInviteCookie("a b;c=d", doc);

    expect(doc.cookie).toContain(`${INVITE_COOKIE_NAME}=a%20b%3Bc%3Dd`);
  });
});

describe("validateInviteEmail", () => {
  it("accepts an ordinary address", () => {
    expect(validateInviteEmail("neighbour@example.com").valid).toBe(true);
  });

  it("asks for an address when the field is blank", () => {
    const result = validateInviteEmail("   ");
    expect(result.valid).toBe(false);
    expect(result.error).toMatch(/email address/i);
  });

  it.each(["nobody", "@example.com", "two@@example.com", "who@", "a b@example.com"])(
    "refuses %s",
    (value) => {
      expect(validateInviteEmail(value).valid).toBe(false);
    },
  );

  // net/mail.ParseAddress accepts a dotless domain, and a client stricter than
  // the server refuses addresses that would have worked.
  it("accepts a dotless domain rather than being stricter than the server", () => {
    expect(validateInviteEmail("root@localhost").valid).toBe(true);
  });
});

describe("validateInviteNote", () => {
  it("accepts no note at all", () => {
    expect(validateInviteNote("").valid).toBe(true);
  });

  it("accepts a note right on the limit", () => {
    expect(validateInviteNote("a".repeat(MAX_INVITE_NOTE_LENGTH)).valid).toBe(true);
  });

  it("refuses a note past the limit, saying by how much", () => {
    const result = validateInviteNote("a".repeat(MAX_INVITE_NOTE_LENGTH + 1));
    expect(result.valid).toBe(false);
    expect(result.error).toContain(`${MAX_INVITE_NOTE_LENGTH + 1} characters`);
  });

  // Counted the way Go counts runes: an emoji is one, not the two UTF-16 units
  // JS string length reports, so the counter cannot claim a note is over a
  // limit the server considers it inside.
  it("counts an astral character as one rune, as the server does", () => {
    expect(remainingNoteChars("🔔")).toBe(MAX_INVITE_NOTE_LENGTH - 1);
    expect(validateInviteNote("🔔".repeat(MAX_INVITE_NOTE_LENGTH)).valid).toBe(true);
  });
});

describe("INVITE_CONSEQUENCE", () => {
  it("tells the sender the invitation is a vouch, and what it costs", () => {
    expect(INVITE_CONSEQUENCE).toContain("vouching for them");
    expect(INVITE_CONSEQUENCE).toContain("today's three vouches");
  });

  // The prose spells the number out, so nothing but a test ties it to the
  // constant the server actually enforces.
  it("spells the same daily budget the vouch limit enforces", () => {
    expect(DAILY_VOUCH_LIMIT).toBe(3);
  });
});

describe("describeInviteExpiry", () => {
  it("counts whole days down for an invitation with time left", () => {
    expect(describeInviteExpiry(days(5), NOW)).toBe("Expires in 5 days");
  });

  it("rounds a part day up rather than losing it", () => {
    expect(describeInviteExpiry(days(4.2), NOW)).toBe("Expires in 5 days");
  });

  it("says today on the last day rather than in 0 days", () => {
    expect(describeInviteExpiry(days(0.5), NOW)).toBe("Expires today");
  });

  it("says tomorrow on the day before", () => {
    expect(describeInviteExpiry(days(1.5), NOW)).toBe("Expires tomorrow");
  });

  // Nothing sweeps an expiry; a page left open overnight must not go on
  // promising a link that has stopped working.
  it("reads an expiry behind the clock as expired", () => {
    expect(describeInviteExpiry(days(-1), NOW)).toBe("Expired");
  });

  it("says nothing for a missing or unparseable expiry", () => {
    expect(describeInviteExpiry(undefined, NOW)).toBe("");
    expect(describeInviteExpiry("not a date", NOW)).toBe("");
  });
});

describe("describeInviteStatus", () => {
  it("counts an open invitation down", () => {
    expect(describeInviteStatus(invite({ expires_at: days(3) }), NOW)).toBe("Expires in 3 days");
  });

  // The status still says open, but the expiry has passed — the honest reading
  // is the clock's, not the stale response's.
  it("reads an open invitation past its expiry as expired", () => {
    expect(describeInviteStatus(invite({ expires_at: days(-2) }), NOW)).toBe("Expired");
  });

  it("names who an accepted invitation turned into", () => {
    const accepted = invite({ status: "accepted", consumed_by_display_name: "Grace Hopper" });
    expect(describeInviteStatus(accepted, NOW)).toBe("Accepted — they joined as Grace Hopper");
  });

  // The common case on the day somebody joins: they have not set a name yet.
  it("still reports an acceptance when the newcomer has no display name", () => {
    const accepted = invite({ status: "accepted", consumed_by_display_name: "  " });
    expect(describeInviteStatus(accepted, NOW)).toBe("Accepted — they have joined");
  });

  it("dates an expired invitation", () => {
    const expired = invite({ status: "expired", expires_at: "2026-02-14T00:00:00Z" });
    expect(describeInviteStatus(expired, NOW)).toMatch(/^Expired .* — nobody used it$/);
  });

  it("says a revoked invitation was withdrawn by its sender", () => {
    expect(describeInviteStatus(invite({ status: "revoked" }), NOW)).toContain("withdrew");
  });

  // The server can learn a fifth status; showing the unfamiliar word is honest
  // where showing nothing is not.
  it("shows a status this build has never heard of as it came", () => {
    const odd = invite({ status: "bounced" as Invite["status"] });
    expect(describeInviteStatus(odd, NOW)).toBe("Bounced");
  });
});

describe("canRevokeInvite", () => {
  it("offers to withdraw an open invitation", () => {
    expect(canRevokeInvite(invite())).toBe(true);
  });

  // Once somebody has arrived, taking the endorsement back is a vouch revoke,
  // with the penalty that carries — not a line struck off this list.
  it.each(["accepted", "expired", "revoked"] as const)("does not offer to withdraw a %s one", (status) => {
    expect(canRevokeInvite(invite({ status }))).toBe(false);
  });

  it("refuses nothing at all", () => {
    expect(canRevokeInvite(null)).toBe(false);
  });
});

describe("describeInviteCount", () => {
  it("counts only the invitations still out", () => {
    const invites = [invite(), invite({ id: "i2", status: "accepted" }), invite({ id: "i3" })];
    expect(describeInviteCount(invites)).toBe("2 still waiting");
  });

  it("says one without pluralising it", () => {
    expect(describeInviteCount([invite()])).toBe("1 still waiting");
  });

  it("says nothing when nothing is out", () => {
    expect(describeInviteCount([invite({ status: "accepted" })])).toBe("");
    expect(describeInviteCount([])).toBe("");
    expect(describeInviteCount(null)).toBe("");
  });
});

describe("inviteErrorMessage", () => {
  // The server's own sentence distinguishes already-invited from a bad address
  // better than any guess made from the status alone.
  it("passes the server's refusal through, tidied into a sentence", () => {
    const err = apiError(400, "validation error: that neighbour has already been invited");
    expect(inviteErrorMessage(err)).toBe("That neighbour has already been invited.");
  });

  it("names the shared budget when today's is spent", () => {
    const message = inviteErrorMessage(apiError(429));
    expect(message).toContain(`${DAILY_VOUCH_LIMIT} a day`);
    expect(message).toContain("tomorrow");
  });

  it("explains a live invitation to the same address", () => {
    expect(inviteErrorMessage(apiError(409))).toMatch(/already an invitation/i);
  });

  // The button is only offered to somebody who could vouch, so a 403 means
  // their standing changed while the page was open.
  it("names the threshold when the sender's standing no longer reaches it", () => {
    expect(inviteErrorMessage(apiError(403))).toContain(`${VOUCHING_THRESHOLD}`);
  });

  // A 403 that has nothing to do with standing; sending them after vouches
  // would be sending them nowhere.
  it("tells an unverified member to open their inbox instead", () => {
    const message = inviteErrorMessage(apiError(403, "email not verified"));
    expect(message).toMatch(/verif/i);
    expect(message).not.toContain(`${VOUCHING_THRESHOLD}`);
  });

  it("has a plain fallback for a failure with nothing to say", () => {
    expect(inviteErrorMessage(apiError(500))).toBe(
      "The invitation could not be sent. Please try again.",
    );
    expect(inviteErrorMessage(null)).toBe("The invitation could not be sent. Please try again.");
  });

  it("speaks about withdrawing when that is what failed", () => {
    expect(inviteErrorMessage(apiError(500), "revoke")).toMatch(/could not be withdrawn/);
    expect(inviteErrorMessage(apiError(404), "revoke")).toMatch(/no longer there/);
    expect(inviteErrorMessage(apiError(403), "revoke")).toMatch(/not yours to withdraw/);
  });
});

describe("invitedGreeting", () => {
  it("names the neighbour and the town", () => {
    expect(invitedGreeting("Ada Lovelace", "Millbrook")).toBe(
      "Ada Lovelace invited you to join Millbrook — welcome.",
    );
  });

  it("still greets somebody when the inviter has set no name", () => {
    expect(invitedGreeting("  ", "Millbrook")).toBe(
      "A neighbour invited you to join Millbrook — welcome.",
    );
  });
});
