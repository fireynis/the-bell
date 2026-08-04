import type { ProposalSummary } from "../../api/types";
import { approvalPercent } from "../../lib/proposal";
import AdminSection from "./AdminSection";

/** CouncilVotesSection lists open proposals with their running tally. */
export default function CouncilVotesSection({
  proposals,
  onVote,
  voting,
}: {
  proposals: ProposalSummary[];
  onVote: (proposalId: string, vote: "approve" | "reject") => void;
  voting: string | null;
}) {
  return (
    <AdminSection
      title="Council Proposals"
      isEmpty={proposals.length === 0}
      emptyMessage="No open proposals at this time."
    >
      <ul className="space-y-4">
        {proposals.map((proposal) => (
          <li
            key={proposal.proposal_id}
            className="p-4"
            style={{
              borderWidth: "1px",
              borderStyle: "solid",
              borderColor: "var(--color-border)",
              borderRadius: "var(--radius-md)",
            }}
          >
            <div className="flex items-start justify-between">
              <div>
                <p className="text-sm font-medium" style={{ color: "var(--color-text)" }}>
                  Proposal {proposal.proposal_id.slice(0, 8)}
                </p>
                <div
                  className="mt-1 flex gap-4 text-xs"
                  style={{ color: "var(--color-text-secondary)" }}
                >
                  <span style={{ color: "var(--color-success)" }}>
                    {proposal.approve_count} approve
                  </span>
                  <span style={{ color: "var(--color-danger)" }}>
                    {proposal.reject_count} reject
                  </span>
                  <span>
                    {proposal.approve_count + proposal.reject_count} /{" "}
                    {proposal.total_council} voted
                  </span>
                </div>
                <div className="mt-2">
                  <div
                    className="h-2 w-48 rounded-full"
                    style={{ backgroundColor: "var(--color-border)" }}
                  >
                    <div
                      className="h-2 rounded-full"
                      style={{
                        backgroundColor: "var(--color-success)",
                        width: `${approvalPercent(proposal)}%`,
                      }}
                    />
                  </div>
                </div>
              </div>
              <div className="flex gap-2">
                <button
                  onClick={() => onVote(proposal.proposal_id, "approve")}
                  disabled={voting === proposal.proposal_id}
                  className="rounded-md px-3 py-1.5 text-sm font-medium disabled:cursor-not-allowed disabled:opacity-50"
                  style={{
                    backgroundColor: "var(--color-success)",
                    color: "var(--color-text-inverse)",
                  }}
                >
                  Approve
                </button>
                <button
                  onClick={() => onVote(proposal.proposal_id, "reject")}
                  disabled={voting === proposal.proposal_id}
                  className="rounded-md px-3 py-1.5 text-sm font-medium disabled:cursor-not-allowed disabled:opacity-50"
                  style={{
                    backgroundColor: "var(--color-danger)",
                    color: "var(--color-text-inverse)",
                  }}
                >
                  Reject
                </button>
              </div>
            </div>
          </li>
        ))}
      </ul>
    </AdminSection>
  );
}
