import { describe, expect, it } from "vitest";
import type { ProposalSummary } from "../api/types";
import { applyVote, approvalPercent } from "./proposal";

function proposal(overrides: Partial<ProposalSummary> = {}): ProposalSummary {
  return {
    proposal_id: "prop-1",
    approve_count: 0,
    reject_count: 0,
    total_council: 5,
    status: "pending",
    votes: [],
    ...overrides,
  };
}

describe("approvalPercent", () => {
  it("reports no progress when nobody has approved", () => {
    expect(approvalPercent(proposal({ approve_count: 0, total_council: 5 }))).toBe(0);
  });

  it("reports a partial share of the council", () => {
    expect(approvalPercent(proposal({ approve_count: 2, total_council: 5 }))).toBe(40);
  });

  it("reports a full bar when the whole council has approved", () => {
    expect(approvalPercent(proposal({ approve_count: 5, total_council: 5 }))).toBe(100);
  });

  // A vacant council is a real state, and dividing by it produced NaN, which
  // React writes into the style as "NaN%" — an invalid width the browser drops,
  // collapsing the bar with no error anywhere.
  it("reports zero rather than NaN when the council has no members", () => {
    const percent = approvalPercent(proposal({ approve_count: 0, total_council: 0 }));
    expect(percent).toBe(0);
    expect(Number.isNaN(percent)).toBe(false);
  });

  it("reports zero when the council size is negative", () => {
    expect(approvalPercent(proposal({ approve_count: 3, total_council: -5 }))).toBe(0);
  });

  // More approvals than seats should not render a bar wider than its track.
  it("caps at 100 when approvals exceed the council size", () => {
    expect(approvalPercent(proposal({ approve_count: 9, total_council: 5 }))).toBe(100);
  });

  it("reports zero for a negative approval count", () => {
    expect(approvalPercent(proposal({ approve_count: -2, total_council: 5 }))).toBe(0);
  });

  it("reports zero for a malformed tally rather than throwing", () => {
    const malformed = { approve_count: Number.NaN, total_council: 5 };
    expect(approvalPercent(malformed)).toBe(0);
  });

  it("reports zero when the counts are missing entirely", () => {
    const missing = {} as Pick<ProposalSummary, "approve_count" | "total_council">;
    expect(approvalPercent(missing)).toBe(0);
  });
});

describe("applyVote", () => {
  it("replaces the voted proposal with the server's updated copy", () => {
    const before = [proposal({ proposal_id: "a" }), proposal({ proposal_id: "b" })];
    const updated = proposal({ proposal_id: "a", approve_count: 3 });

    const after = applyVote(before, updated);

    expect(after).toHaveLength(2);
    expect(after[0].approve_count).toBe(3);
  });

  it("leaves the other proposals untouched", () => {
    const other = proposal({ proposal_id: "b", approve_count: 1 });
    const after = applyVote([proposal({ proposal_id: "a" }), other], proposal({ proposal_id: "a" }));

    expect(after[1]).toBe(other);
  });

  // Casting the deciding vote closes the proposal, so it must leave the open
  // list rather than sitting there still offering vote buttons.
  it("removes a proposal that the vote just approved", () => {
    const before = [proposal({ proposal_id: "a" }), proposal({ proposal_id: "b" })];
    const closed = proposal({ proposal_id: "a", status: "approved" });

    const after = applyVote(before, closed);

    expect(after.map((p) => p.proposal_id)).toEqual(["b"]);
  });

  it("removes a proposal that the vote just rejected", () => {
    const after = applyVote(
      [proposal({ proposal_id: "a" })],
      proposal({ proposal_id: "a", status: "rejected" }),
    );

    expect(after).toEqual([]);
  });

  // The list is what this council member was shown; a vote on a proposal that
  // has since closed and left the list is not a reason to put it back.
  it("does not reinsert a proposal that is no longer in the list", () => {
    const after = applyVote([proposal({ proposal_id: "b" })], proposal({ proposal_id: "gone" }));

    expect(after.map((p) => p.proposal_id)).toEqual(["b"]);
  });

  it("drops proposals that were already closed in the existing list", () => {
    const before = [proposal({ proposal_id: "a" }), proposal({ proposal_id: "stale", status: "approved" })];

    const after = applyVote(before, proposal({ proposal_id: "a" }));

    expect(after.map((p) => p.proposal_id)).toEqual(["a"]);
  });

  it("returns an empty list when there were no proposals", () => {
    expect(applyVote([], proposal())).toEqual([]);
  });

  it("still prunes closed proposals when the response is malformed", () => {
    const before = [proposal({ proposal_id: "a" }), proposal({ proposal_id: "b", status: "approved" })];

    const after = applyVote(before, null);

    expect(after.map((p) => p.proposal_id)).toEqual(["a"]);
  });

  it("survives a missing list rather than throwing", () => {
    const absent = null as unknown as ProposalSummary[];
    expect(applyVote(absent, proposal())).toEqual([]);
  });

  it("does not mutate the list it was given", () => {
    const before = [proposal({ proposal_id: "a", approve_count: 0 })];

    applyVote(before, proposal({ proposal_id: "a", approve_count: 7 }));

    expect(before[0].approve_count).toBe(0);
  });
});
