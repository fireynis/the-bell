import { describe, expect, it } from "vitest";
import type { User } from "../api/types";
import { canPost, canVouch } from "./trust";
import {
  POSTING_THRESHOLD,
  VOUCHING_THRESHOLD,
  postingBlockReason,
  vouchingBlockReason,
} from "./gating";

type Actor = Pick<User, "trust_score" | "role" | "is_active">;

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

  for (const role of roles) {
    for (const score of scores) {
      for (const isActive of [true, false]) {
        const subject = actor({ role, trust_score: score, is_active: isActive });
        const label = `${role} with score ${score}${isActive ? "" : " (suspended)"}`;

        it(`gives a posting reason exactly when canPost refuses: ${label}`, () => {
          expect(postingBlockReason(subject) === null).toBe(canPost(subject));
        });

        it(`gives a vouching reason exactly when canVouch refuses: ${label}`, () => {
          expect(vouchingBlockReason(subject) === null).toBe(canVouch(subject));
        });
      }
    }
  }
});
