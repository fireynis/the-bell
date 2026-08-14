import { describe, expect, it } from "vitest";
import type { User } from "../api/types";
import { formatDateTime } from "./time";
import { canPost, canVouch } from "./trust";
import {
  PENDING_PATH_TO_MEMBER,
  PENDING_WELCOME,
  POSTING_THRESHOLD,
  VOUCHING_THRESHOLD,
  awaitingWelcome,
  postingBlockReason,
  vouchingBlockReason,
} from "./gating";

type Actor = Pick<User, "trust_score" | "role" | "is_active" | "muted_until">;

function actor(overrides: Partial<Actor> = {}): Actor {
  return {
    trust_score: 50,
    role: "member",
    is_active: true,
    ...overrides,
  };
}

describe("postingBlockReason", () => {
  it("allows a member at exactly the posting threshold", () => {
    expect(postingBlockReason(actor({ trust_score: POSTING_THRESHOLD }))).toBeNull();
  });

  it("blocks a member one point below the posting threshold", () => {
    expect(postingBlockReason(actor({ trust_score: POSTING_THRESHOLD - 1 }))).not.toBeNull();
  });

  it("tells the user the threshold they need to reach", () => {
    const reason = postingBlockReason(actor({ trust_score: 10 }));
    expect(reason).toContain(String(POSTING_THRESHOLD));
  });

  it("tells the user their own score", () => {
    expect(postingBlockReason(actor({ trust_score: 12 }))).toContain("12");
  });

  it("explains that vouches are how trust grows", () => {
    expect(postingBlockReason(actor({ trust_score: 0 }))).toContain("vouch");
  });

  it("asks a signed-out visitor to sign in", () => {
    expect(postingBlockReason(null)).toContain("signed in");
  });

  // A suspended user's score is beside the point — telling them to earn trust
  // would send them off chasing vouches that would not unblock them.
  it("blames suspension rather than trust score for a suspended user", () => {
    const reason = postingBlockReason(actor({ is_active: false, trust_score: 0 }));
    expect(reason).toContain("suspended");
    expect(reason).not.toContain(String(POSTING_THRESHOLD));
  });

  it("blames the ban rather than trust score for a banned user", () => {
    const reason = postingBlockReason(actor({ role: "banned", trust_score: 100 }));
    expect(reason).toContain("banned");
  });

  it("tells a pending user they are waiting on council approval", () => {
    const reason = postingBlockReason(actor({ role: "pending", trust_score: 100 }));
    expect(reason).toContain("approval");
  });

  // Both routes out of pending are real: any member's vouch promotes on the
  // spot, and the council can approve directly while the town is in bootstrap.
  // Naming only the council sends someone to wait on a body that may never look
  // at them.
  it("names both ways out of pending", () => {
    const reason = postingBlockReason(actor({ role: "pending" }));
    expect(reason).toContain("vouching");
    expect(reason).toContain("council approval");
  });

  it("tells a pending user the same story the welcome banner tells", () => {
    expect(postingBlockReason(actor({ role: "pending" }))).toContain(PENDING_PATH_TO_MEMBER);
    expect(PENDING_WELCOME.next).toContain(PENDING_PATH_TO_MEMBER);
  });

  // 29.7 is below a threshold of 30, so rounding it for display would print
  // "You need 30. Yours is 30" and read as a broken gate.
  it("floors a fractional score so it never appears to meet the threshold", () => {
    const reason = postingBlockReason(actor({ trust_score: POSTING_THRESHOLD - 0.3 }));
    expect(reason).toContain(String(POSTING_THRESHOLD - 1));
    expect(reason).not.toContain(`Yours is ${POSTING_THRESHOLD}`);
  });

  it("treats a malformed score as zero rather than throwing", () => {
    expect(postingBlockReason(actor({ trust_score: Number.NaN }))).toContain("Yours is 0");
  });
});

// AuthContext used to answer a failed profile fetch with `user = null`, which
// is the same thing it says about a visitor with no session at all — so a
// signed-in member was told to sign in, and signing in returned them to the
// same message.
describe("a profile that could not be loaded", () => {
  it("does not tell a signed-in member to sign in", () => {
    const reason = postingBlockReason(null, { profileUnavailable: true });
    expect(reason).not.toContain("signed in");
  });

  it("says what actually happened and what to do about it", () => {
    const reason = postingBlockReason(null, { profileUnavailable: true });
    expect(reason).toContain("could not load your account");
    expect(reason).toContain("Refresh");
  });

  it("still asks a genuine visitor to sign in", () => {
    expect(postingBlockReason(null, { profileUnavailable: false })).toContain("signed in");
    expect(postingBlockReason(null, {})).toContain("signed in");
  });

  it("names the action it is talking about, like every other reason", () => {
    expect(vouchingBlockReason(null, { profileUnavailable: true })).toContain("vouch for someone");
  });

  // The flag describes why there is no actor, so it has nothing to say about
  // one we do have — a loaded member is judged on their own record either way.
  it("changes nothing for a member whose profile did load", () => {
    const member = actor({ trust_score: 0 });
    expect(postingBlockReason(member, { profileUnavailable: true })).toBe(
      postingBlockReason(member),
    );
  });
});

describe("awaitingWelcome", () => {
  it("greets a pending member", () => {
    expect(awaitingWelcome(actor({ role: "pending" }))).toBe(true);
  });

  it.each(["member", "moderator", "council", "banned"])("does not greet a %s", (role) => {
    expect(awaitingWelcome(actor({ role }))).toBe(false);
  });

  // The suspension is the more specific thing to say, and a warm "someone will
  // vouch for you soon" over the top of it would be false comfort.
  it("does not greet a suspended pending account", () => {
    expect(awaitingWelcome(actor({ role: "pending", is_active: false }))).toBe(false);
  });

  it("greets nobody when nobody is signed in", () => {
    expect(awaitingWelcome(null)).toBe(false);
  });
});

describe("the pending welcome", () => {
  it("says what pending means, what ends it, and what to do meanwhile", () => {
    expect(PENDING_WELCOME.meaning).toContain("vouched");
    expect(PENDING_WELCOME.next).toContain(PENDING_PATH_TO_MEMBER);
    expect(PENDING_WELCOME.meanwhile).toContain("profile");
  });

  // Completing a profile is the one thing a pending member can act on, so the
  // banner has to offer somewhere to do it.
  it("offers a way to complete the profile", () => {
    expect(PENDING_WELCOME.profileCta.length).toBeGreaterThan(0);
  });

  it("never calls the member rejected or refused", () => {
    const all = Object.values(PENDING_WELCOME).join(" ").toLowerCase();
    for (const word of ["rejected", "denied", "refused", "not allowed"]) {
      expect(all).not.toContain(word);
    }
  });
});

describe("vouchingBlockReason", () => {
  it("allows a member at exactly the vouching threshold", () => {
    expect(vouchingBlockReason(actor({ trust_score: VOUCHING_THRESHOLD }))).toBeNull();
  });

  it("blocks a member one point below the vouching threshold", () => {
    expect(vouchingBlockReason(actor({ trust_score: VOUCHING_THRESHOLD - 1 }))).not.toBeNull();
  });

  // The two gates sit at different heights, so a score can clear posting and
  // still fall short of vouching.
  it("blocks vouching for a score that is enough to post", () => {
    const between = actor({ trust_score: POSTING_THRESHOLD });
    expect(postingBlockReason(between)).toBeNull();
    expect(vouchingBlockReason(between)).not.toBeNull();
  });

  it("quotes the higher vouching threshold", () => {
    expect(vouchingBlockReason(actor({ trust_score: 0 }))).toContain(String(VOUCHING_THRESHOLD));
  });
});

/**
 * A mute reaches the composer, which it did not before: the Actor type picked
 * everything domain.User.CanPost reads except muted_until, so a muted member
 * got an enabled box, wrote a whole post, and was answered with a bare 403.
 */
describe("a muted member", () => {
  const NOW = new Date("2026-08-14T12:00:00Z");
  const LATER = "2026-08-15T09:30:00Z";

  const muted = (until: string) => actor({ muted_until: until });

  it("cannot post while the mute is in force", () => {
    expect(postingBlockReason(muted(LATER), { now: NOW })).not.toBeNull();
  });

  it("is told when it ends, in the same words the moderation pages use", () => {
    const reason = postingBlockReason(muted(LATER), { now: NOW });
    expect(reason).toContain(formatDateTime(LATER));
  });

  // Being silenced is not being cut off. A mute stops posting and nothing else,
  // which is exactly what the action dialog promised the moderator who applied
  // it — so the member is told the same thing rather than left to guess.
  it("is told what still works", () => {
    const reason = postingBlockReason(muted(LATER), { now: NOW });
    expect(reason).toContain("read the feed");
    expect(reason).toContain("react");
  });

  // Nothing sweeps an expired mute; the comparison against the clock is the
  // whole mechanism, so it has to stop applying on its own.
  it("can post again once the mute has run out", () => {
    expect(postingBlockReason(muted("2026-08-14T11:59:00Z"), { now: NOW })).toBeNull();
  });

  it("is not muted by a timestamp the API sent malformed", () => {
    expect(postingBlockReason(muted("not a date"), { now: NOW })).toBeNull();
  });

  // domain.User.CanVouch does not consult a mute, so neither does this: a mute
  // silences somebody, it does not stop them saying they know their neighbour.
  it("can still vouch", () => {
    expect(
      vouchingBlockReason(actor({ trust_score: VOUCHING_THRESHOLD, muted_until: LATER }), {
        now: NOW,
      }),
    ).toBeNull();
  });

  // The mute is the specific, dated thing in the way. Sending them off to earn
  // trust would be advice that cannot unblock them.
  it("blames the mute rather than a low trust score", () => {
    const reason = postingBlockReason(actor({ trust_score: 0, muted_until: LATER }), { now: NOW });
    expect(reason).toContain("muted");
    expect(reason).not.toContain("trust score");
  });

  // Suspension outranks it: a suspended member cannot read the feed's controls
  // either, so "you can still read and react" would be the wrong promise.
  it("is told about a suspension first", () => {
    const reason = postingBlockReason(
      actor({ is_active: false, muted_until: LATER }),
      { now: NOW },
    );
    expect(reason).toContain("suspended");
  });
});

// The message and the gate must never disagree: a reason shown beside an
// enabled button, or a disabled button with no explanation, is worse than
// either alone. Rather than trusting that both were updated together, the two
// are checked against each other across the whole matrix of actors.
describe("block reasons agree with the trust predicates", () => {
  const roles: Array<Actor["role"]> = ["pending", "member", "moderator", "council", "banned"];
  const scores = [
    0,
    POSTING_THRESHOLD - 1,
    POSTING_THRESHOLD,
    VOUCHING_THRESHOLD - 1,
    VOUCHING_THRESHOLD,
    100,
  ];
  // Undefined, in force, and lapsed — the mute has to be in the matrix or the
  // agreement it is checking would hold everywhere except where it is new.
  const mutes: Array<string | undefined> = [
    undefined,
    "2100-01-01T00:00:00Z",
    "2000-01-01T00:00:00Z",
  ];

  for (const role of roles) {
    for (const score of scores) {
      for (const isActive of [true, false]) {
        for (const mutedUntil of mutes) {
          const subject = actor({
            role,
            trust_score: score,
            is_active: isActive,
            muted_until: mutedUntil,
          });
          const label = `${role} with score ${score}${isActive ? "" : " (suspended)"}${
            mutedUntil ? ` muted until ${mutedUntil}` : ""
          }`;

          it(`gives a posting reason exactly when canPost refuses: ${label}`, () => {
            expect(postingBlockReason(subject) === null).toBe(canPost(subject));
          });

          it(`gives a vouching reason exactly when canVouch refuses: ${label}`, () => {
            expect(vouchingBlockReason(subject) === null).toBe(canVouch(subject));
          });
        }
      }
    }
  }
});
