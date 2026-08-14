import { useState } from "react";
import type { Proposal } from "../../api/types";
import {
  NO_OPEN_PROPOSALS,
  decidedProposals,
  decidedSummary,
  openProposals,
  proposalTitle,
} from "../../lib/proposal";
import AdminSection from "./AdminSection";
import NewProposalDialog from "./NewProposalDialog";
import ProposalCard from "./ProposalCard";

/**
 * The town hall: what the council is deciding, and what it has decided.
 *
 * Both lists come out of one array of proposals rather than being fetched and
 * kept apart, because a vote can move a proposal from the first to the second
 * without a reload — the deciding vote closes it and carries it out in the same
 * request. Deriving both from the array is what makes that move happen on screen
 * by itself.
 *
 * The page it sits on is council-gated, which is the only reason none of this
 * checks who is reading: the server refuses these routes to everyone else.
 */
export default function ProposalsSection({
  proposals,
  viewerId,
  onVote,
  voting,
  onCreated,
}: {
  proposals: Proposal[];
  viewerId: string | null;
  onVote: (proposalId: string, vote: "approve" | "reject") => void;
  /** Which proposal has a vote in flight, if any. */
  voting: string | null;
  /** Called once a new proposal is on the board, so the page can reload. */
  onCreated: () => void;
}) {
  const [raising, setRaising] = useState(false);

  const open = openProposals(proposals);
  const decided = decidedProposals(proposals);

  return (
    <>
    <AdminSection
      title="Town Hall"
      isEmpty={open.length === 0 && decided.length === 0}
      emptyMessage={NO_OPEN_PROPOSALS}
      action={
        <button
          type="button"
          onClick={() => setRaising(true)}
          className="rounded-md px-3 py-1.5 text-sm font-medium"
          style={{
            backgroundColor: "var(--color-primary)",
            color: "var(--color-text-inverse)",
          }}
        >
          Raise a proposal
        </button>
      }
    >
      <>
        {open.length === 0 ? (
          <p className="text-sm" style={{ color: "var(--color-text-secondary)" }}>
            {NO_OPEN_PROPOSALS}
          </p>
        ) : (
          <ul className="space-y-4">
            {open.map((proposal) => (
              <ProposalCard
                key={proposal.id}
                proposal={proposal}
                viewerId={viewerId}
                onVote={onVote}
                voting={voting === proposal.id}
              />
            ))}
          </ul>
        )}

        {decided.length > 0 && (
          <div className="mt-6">
            <h3
              className="text-sm font-semibold"
              style={{ color: "var(--color-text-secondary)" }}
            >
              Already decided
            </h3>
            {/*
              A short record rather than a second set of cards. What matters
              afterwards is what the council decided and when — the rationale and
              the running tally were arguments for a vote that has happened.
            */}
            <ul className="mt-2 space-y-1">
              {decided.map((proposal) => (
                <li
                  key={proposal.id}
                  className="flex flex-wrap items-baseline justify-between gap-2 text-sm"
                >
                  <span style={{ color: "var(--color-text-secondary)" }}>
                    {proposalTitle(proposal)}
                  </span>
                  <span
                    className="text-xs"
                    style={{
                      color:
                        proposal.status === "passed"
                          ? "var(--color-success)"
                          : "var(--color-text-tertiary)",
                    }}
                  >
                    {decidedSummary(proposal)}
                  </span>
                </li>
              ))}
            </ul>
          </div>
        )}
      </>
    </AdminSection>

    {/*
      Outside the section on purpose: AdminSection renders no children while it
      is empty, and an empty town hall is exactly when somebody reaches for the
      button that opens this.
    */}
    {raising && <NewProposalDialog onClose={() => setRaising(false)} onCreated={onCreated} />}
    </>
  );
}
