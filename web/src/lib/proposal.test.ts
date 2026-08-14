import { describe, expect, it } from "vitest";
import type { ApiError, DirectoryUser, Proposal } from "../api/types";
import {
  BOOTSTRAP_EXIT_THRESHOLD,
  MAX_RATIONALE_LENGTH,
  applyProposalUpdate,
  canVoteOn,
  decidedProposals,
  decidedSummary,
  eligibleCandidates,
  isOwnRemoval,
  majorityOf,
  needsTarget,
  openProposals,
  proposalConsequence,
  proposalErrorMessage,
  proposalTitle,
  tallySentence,
  validateRationale,
  voteBlockReason,
} from "./proposal";

/**
 * The town hall is where a council votes on things that happen the moment the
 * vote lands. These pin the sentences that say what is being decided and what
 * passing will do, and the rules that decide who is offered a vote at all.
 */

function proposal(overrides: Partial<Proposal> = {}): Proposal {
  return {
    id: "prop-1",
    type: "council_promotion",
    target_user_id: "user-1",
    target_display_name: "Ada Lovelace",
    rationale: "She has been moderating for a year.",
    created_by: "user-9",
    created_by_display_name: "Grace Hopper",
    status: "open",
    created_at: "2026-08-01T10:00:00Z",
    approve_count: 0,
    reject_count: 0,
    council_size: 5,
    my_vote: null,
    ...overrides,
  };
}

function directoryUser(overrides: Partial<DirectoryUser> = {}): DirectoryUser {
  return {
    id: "user-1",
    display_name: "Ada Lovelace",
    role: "member",
    joined_at: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

describe("proposalTitle", () => {
  it("names a promotion in plain words", () => {
    expect(proposalTitle(proposal())).toBe("Promote Ada Lovelace to council");
  });

  it("names a removal in plain words", () => {
    expect(proposalTitle(proposal({ type: "council_removal" }))).toBe(
      "Remove Ada Lovelace from council",
    );
  });

  // The one proposal about the town rather than about a person, so it names no
  // one even though the DTO could carry a stale target.
  it("names a bootstrap re-entry without naming anybody", () => {
    expect(proposalTitle(proposal({ type: "bootstrap_reentry" }))).toBe(
      "Reopen the town to council approvals",
    );
  });

  it("falls back to the id prefix for a target with no display name", () => {
    const title = proposalTitle(
      proposal({ target_display_name: "", target_user_id: "0193a7b2-aaaa-7000-8000-000000000000" }),
    );
    expect(title).toBe("Promote 0193a7b2... to council");
  });

  // A server that has learned a fourth kind must leave the council able to see
  // that there is something to decide, rather than a row that looks broken.
  it("says there is something here for a kind it does not recognise", () => {
    const title = proposalTitle(proposal({ type: "town_renaming" }));
    expect(title).toMatch(/does not recognise/);
  });
});

describe("proposalConsequence", () => {
  // Both of these are carried out in the same request as the deciding vote,
  // which is precisely what a council member needs to know beforehand.
  it("warns that a promotion takes effect immediately", () => {
    expect(proposalConsequence("council_promotion")).toMatch(/straight away/);
  });

  it("warns that a removal takes effect immediately, and that the target has no vote", () => {
    const line = proposalConsequence("council_removal");
    expect(line).toMatch(/straight away/);
    expect(line).toMatch(/do not vote on this/);
  });

  it("says what reopening approvals costs, and names the threshold that ends it", () => {
    expect(proposalConsequence("bootstrap_reentry")).toContain(String(BOOTSTRAP_EXIT_THRESHOLD));
  });

  it("admits it cannot describe a kind it does not recognise", () => {
    expect(proposalConsequence("town_renaming")).toMatch(/cannot describe/);
  });
});

describe("tallySentence", () => {
  it("reports turnout and which way it is going", () => {
    expect(
      tallySentence(proposal({ approve_count: 2, reject_count: 1, council_size: 5 })),
    ).toBe("3 of 5 council members have voted — 2 in favour.");
  });

  it("keeps the grammar right for a single vote", () => {
    expect(
      tallySentence(proposal({ approve_count: 1, reject_count: 0, council_size: 5 })),
    ).toBe("1 of 5 council members has voted — 1 in favour.");
  });

  it("says nobody has voted rather than counting to zero", () => {
    expect(tallySentence(proposal({ approve_count: 0, reject_count: 0 }))).toBe(
      "Nobody has voted yet.",
    );
  });

  // A removal excludes its own target from the electorate, so the denominator
  // travels with the proposal and two cards on the same page can differ.
  it("counts against the proposal's own electorate, not the council at large", () => {
    expect(
      tallySentence(proposal({ type: "council_removal", approve_count: 2, reject_count: 0, council_size: 4 })),
    ).toBe("2 of 4 council members have voted — 2 in favour.");
  });

  it("names no denominator it cannot trust", () => {
    const sentence = tallySentence(proposal({ approve_count: 2, council_size: 0 }));
    expect(sentence).toBe("2 votes cast — 2 in favour.");
    expect(sentence).not.toContain("of 0");
  });

  it("survives a malformed tally rather than printing NaN", () => {
    const malformed = {
      approve_count: Number.NaN,
      reject_count: 2,
      council_size: 5,
    };
    expect(tallySentence(malformed)).toBe("2 of 5 council members have voted — 0 in favour.");
  });
});

describe("majorityOf", () => {
  it("is more than half of an odd council", () => {
    expect(majorityOf(5)).toBe(3);
  });

  it("is more than half of an even council", () => {
    expect(majorityOf(4)).toBe(3);
  });

  it("is one for a council of one", () => {
    expect(majorityOf(1)).toBe(1);
  });

  it("is nothing for a council of nobody", () => {
    expect(majorityOf(0)).toBe(0);
    expect(majorityOf(Number.NaN)).toBe(0);
  });
});

describe("voteBlockReason", () => {
  it("offers a vote to a council member who has not cast one", () => {
    expect(voteBlockReason(proposal(), "user-9")).toBeNull();
    expect(canVoteOn(proposal(), "user-9")).toBe(true);
  });

  // The server refuses a second vote outright, so a button offering one would
  // only produce a 409.
  it("refuses a second vote, and says which way the first went", () => {
    expect(voteBlockReason(proposal({ my_vote: "approve" }), "user-9")).toBe("You voted in favour.");
    expect(voteBlockReason(proposal({ my_vote: "reject" }), "user-9")).toBe("You voted against.");
  });

  // They can see it — hiding a proposal about somebody from that somebody would
  // be worse — but they are not in its electorate.
  it("tells a removal target that they do not vote on their own removal", () => {
    const own = proposal({ type: "council_removal", target_user_id: "user-9" });
    expect(voteBlockReason(own, "user-9")).toBe("You don't vote on your own removal.");
    expect(canVoteOn(own, "user-9")).toBe(false);
  });

  it("still lets everybody else vote on that removal", () => {
    const removal = proposal({ type: "council_removal", target_user_id: "user-9" });
    expect(voteBlockReason(removal, "user-3")).toBeNull();
  });

  // Being the subject of a promotion is not the same rule: nothing says the
  // person being promoted sits out, so this side does not invent it.
  it("does not silence somebody named in a promotion", () => {
    expect(voteBlockReason(proposal({ target_user_id: "user-9" }), "user-9")).toBeNull();
  });

  it("offers no vote once the council has decided", () => {
    expect(voteBlockReason(proposal({ status: "passed" }), "user-9")).toMatch(/already been decided/);
    expect(voteBlockReason(proposal({ status: "rejected" }), "user-9")).toMatch(/already been decided/);
  });

  // Only the self-removal rule depends on knowing who is reading. The page is
  // council-gated, so an unidentified viewer means the profile has not arrived
  // yet rather than that a stranger is here — the buttons stay, and the server
  // remains the authority on the vote.
  it("still offers a vote while the viewer's own profile is unknown", () => {
    expect(voteBlockReason(proposal(), null)).toBeNull();
  });
});

describe("isOwnRemoval", () => {
  it("recognises the proposal to unseat the person reading it", () => {
    expect(isOwnRemoval(proposal({ type: "council_removal", target_user_id: "me" }), "me")).toBe(true);
  });

  it("is not somebody else's removal, nor a promotion", () => {
    expect(isOwnRemoval(proposal({ type: "council_removal", target_user_id: "other" }), "me")).toBe(false);
    expect(isOwnRemoval(proposal({ type: "council_promotion", target_user_id: "me" }), "me")).toBe(false);
  });

  it("is nothing at all for an unidentified viewer", () => {
    expect(isOwnRemoval(proposal({ type: "council_removal", target_user_id: "me" }), null)).toBe(false);
  });
});

describe("eligibleCandidates", () => {
  const roll = [
    directoryUser({ id: "m1", role: "moderator", display_name: "Mod One" }),
    directoryUser({ id: "c1", role: "council", display_name: "Council One" }),
    directoryUser({ id: "u1", role: "member", display_name: "Just a member" }),
    directoryUser({ id: "p1", role: "pending", display_name: "Newcomer" }),
  ];

  it("offers moderators to promote", () => {
    expect(eligibleCandidates(roll, "council_promotion").map((u) => u.id)).toEqual(["m1"]);
  });

  it("offers council members to remove", () => {
    expect(eligibleCandidates(roll, "council_removal").map((u) => u.id)).toEqual(["c1"]);
  });

  // Reopening approvals is about the town, so there is nobody to pick.
  it("offers nobody for a proposal that names no person", () => {
    expect(eligibleCandidates(roll, "bootstrap_reentry")).toEqual([]);
  });

  it("survives an empty roll", () => {
    expect(eligibleCandidates([], "council_promotion")).toEqual([]);
  });
});

describe("needsTarget", () => {
  it("is true for the two proposals about a person", () => {
    expect(needsTarget("council_promotion")).toBe(true);
    expect(needsTarget("council_removal")).toBe(true);
  });

  it("is false for the one about the town", () => {
    expect(needsTarget("bootstrap_reentry")).toBe(false);
  });
});

describe("validateRationale", () => {
  it("accepts a reason", () => {
    expect(validateRationale("She has moderated well for a year.")).toEqual({ valid: true });
  });

  it("refuses an empty one, because whitespace is not a reason", () => {
    expect(validateRationale("").valid).toBe(false);
    expect(validateRationale("   ").valid).toBe(false);
  });

  it("accepts a rationale at the limit", () => {
    expect(validateRationale("a".repeat(MAX_RATIONALE_LENGTH))).toEqual({ valid: true });
  });

  it("refuses one character past it, and says by how much", () => {
    const result = validateRationale("a".repeat(MAX_RATIONALE_LENGTH + 1));
    expect(result.valid).toBe(false);
    expect(result.error).toContain(String(MAX_RATIONALE_LENGTH + 1));
  });

  // The contract is written in characters, so a rationale in a language that
  // does not fit in ASCII must not be refused for being multi-byte.
  it("counts characters rather than bytes", () => {
    expect(validateRationale("é".repeat(MAX_RATIONALE_LENGTH))).toEqual({ valid: true });
  });
});

describe("openProposals and decidedProposals", () => {
  const list = [
    proposal({ id: "open-1" }),
    proposal({ id: "passed-1", status: "passed" }),
    proposal({ id: "rejected-1", status: "rejected" }),
  ];

  it("separates what is still to decide from what is history", () => {
    expect(openProposals(list).map((p) => p.id)).toEqual(["open-1"]);
    expect(decidedProposals(list).map((p) => p.id)).toEqual(["passed-1", "rejected-1"]);
  });

  it("keeps the order the server gave", () => {
    expect(decidedProposals(list)[0].id).toBe("passed-1");
  });

  it("survives a missing list rather than throwing", () => {
    const absent = null as unknown as Proposal[];
    expect(openProposals(absent)).toEqual([]);
    expect(decidedProposals(absent)).toEqual([]);
  });
});

describe("applyProposalUpdate", () => {
  it("replaces the voted proposal with the server's copy", () => {
    const before = [proposal({ id: "a" }), proposal({ id: "b" })];

    const after = applyProposalUpdate(before, proposal({ id: "a", approve_count: 3, my_vote: "approve" }));

    expect(after[0].approve_count).toBe(3);
    expect(after[0].my_vote).toBe("approve");
  });

  // The deciding vote carries the proposal out on the spot, so the card has to
  // stop offering votes and start reading as history — without disappearing,
  // which would leave no evidence of what just happened.
  it("keeps a proposal the vote just decided, so it moves to the history", () => {
    const before = [proposal({ id: "a" })];

    const after = applyProposalUpdate(before, proposal({ id: "a", status: "passed", decided_at: "2026-08-02T09:00:00Z" }));

    expect(openProposals(after)).toEqual([]);
    expect(decidedProposals(after).map((p) => p.id)).toEqual(["a"]);
  });

  it("leaves the other proposals untouched", () => {
    const other = proposal({ id: "b" });

    const after = applyProposalUpdate([proposal({ id: "a" }), other], proposal({ id: "a" }));

    expect(after[1]).toBe(other);
  });

  it("does not insert a proposal that was not on the page", () => {
    const after = applyProposalUpdate([proposal({ id: "b" })], proposal({ id: "gone" }));

    expect(after.map((p) => p.id)).toEqual(["b"]);
  });

  it("returns the list unchanged for a malformed response", () => {
    const before = [proposal({ id: "a" })];

    expect(applyProposalUpdate(before, null).map((p) => p.id)).toEqual(["a"]);
  });

  it("does not mutate the list it was given", () => {
    const before = [proposal({ id: "a", approve_count: 0 })];

    applyProposalUpdate(before, proposal({ id: "a", approve_count: 7 }));

    expect(before[0].approve_count).toBe(0);
  });
});

describe("decidedSummary", () => {
  it("says what the council decided and when", () => {
    const summary = decidedSummary({ status: "passed", decided_at: "2026-08-02T09:00:00Z" });
    expect(summary).toMatch(/^Passed /);
  });

  it("says rejected for the ones that did not carry", () => {
    expect(decidedSummary({ status: "rejected", decided_at: "2026-08-02T09:00:00Z" })).toMatch(
      /^Rejected /,
    );
  });

  // Better a decision with no date than one dated "Invalid Date".
  it("drops the date rather than printing a broken one", () => {
    expect(decidedSummary({ status: "passed" })).toBe("Passed");
    expect(decidedSummary({ status: "passed", decided_at: "not a date" })).toBe("Passed");
  });
});

describe("proposalErrorMessage", () => {
  const err = (status: number, error = "boom"): ApiError => ({ status, error });

  // Both a second vote and a proposal decided since the page loaded arrive as a
  // 409, and in both cases trying again is not the fix.
  it("tells a council member to reload when the vote has already been settled", () => {
    expect(proposalErrorMessage(err(409), "vote")).toMatch(/reload/i);
  });

  it("passes on what the server said about a rejected proposal", () => {
    expect(proposalErrorMessage(err(400, "validation error: rationale is required"), "create")).toBe(
      "Rationale is required.",
    );
  });

  it("names the unopened verification email rather than the council rules", () => {
    expect(proposalErrorMessage(err(403, "email not verified"), "vote")).toMatch(/verif/i);
  });

  it("says which action failed when there is nothing else to say", () => {
    expect(proposalErrorMessage(err(500), "vote")).toMatch(/vote could not be recorded/i);
    expect(proposalErrorMessage(err(500), "create")).toMatch(/proposal could not be raised/i);
  });
});
