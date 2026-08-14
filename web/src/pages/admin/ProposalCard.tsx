import type { Proposal } from "../../api/types";
import Spinner from "../../components/Spinner";
import { personName } from "../../lib/people";
import {
  canVoteOn,
  isOwnRemoval,
  proposalConsequence,
  proposalTitle,
  tallySentence,
  voteBlockReason,
} from "../../lib/proposal";
import { formatRelativeTime } from "../../lib/time";

/**
 * One open proposal, with everything a council member needs to decide it: what
 * is being proposed, who raised it and why, what passing will do, where the vote
 * stands, and their own two buttons.
 *
 * The consequence line is not decoration. A promotion or a removal that reaches
 * a majority is carried out inside the same request as the deciding vote — there
 * is no confirmation step and nothing to undo — so the card says so before the
 * buttons rather than after them.
 */
export default function ProposalCard({
  proposal,
  viewerId,
  onVote,
  voting,
}: {
  proposal: Proposal;
  /** The signed-in council member, which decides only the self-removal rule. */
  viewerId: string | null;
  onVote: (proposalId: string, vote: "approve" | "reject") => void;
  /** True while this proposal's own vote is in flight. */
  voting: boolean;
}) {
  const title = proposalTitle(proposal);
  const blocked = voteBlockReason(proposal, viewerId);
  const canVote = canVoteOn(proposal, viewerId) && !voting;
  // The one case that shows no buttons at all rather than disabled ones: there
  // is no vote here to be unable to cast, so offering greyed-out controls would
  // misdescribe the rule as a temporary condition.
  const ownRemoval = isOwnRemoval(proposal, viewerId);

  const voteButton = (vote: "approve" | "reject", label: string, colorVar: string) => (
    <button
      type="button"
      onClick={() => onVote(proposal.id, vote)}
      disabled={!canVote}
      // Named with the proposal, because a page of open proposals otherwise
      // offers half a dozen buttons all called "Approve".
      aria-label={`${label}: ${title}`}
      // The pressed state is how a screen reader conveys "this is the way you
      // voted" on a control that is now disabled.
      aria-pressed={proposal.my_vote === vote}
      className="rounded-md px-3 py-1.5 text-sm font-medium disabled:cursor-not-allowed disabled:opacity-50"
      style={{ backgroundColor: colorVar, color: "var(--color-text-inverse)" }}
    >
      {label}
    </button>
  );

  return (
    <li
      className="p-4"
      style={{
        borderWidth: "1px",
        borderStyle: "solid",
        borderColor: "var(--color-border)",
        borderRadius: "var(--radius-md)",
      }}
    >
      <h3 className="text-sm font-semibold" style={{ color: "var(--color-text)" }}>
        {title}
      </h3>

      <p className="mt-0.5 text-xs" style={{ color: "var(--color-text-tertiary)" }}>
        Raised by {personName(proposal.created_by_display_name, proposal.created_by)}
        {" · "}
        {formatRelativeTime(proposal.created_at, { suffix: true })}
      </p>

      {proposal.rationale && (
        <p
          className="mt-2 text-sm leading-relaxed"
          style={{ color: "var(--color-text-secondary)" }}
        >
          {proposal.rationale}
        </p>
      )}

      <p className="mt-2 text-xs leading-relaxed" style={{ color: "var(--color-text-tertiary)" }}>
        {proposalConsequence(proposal.type)}
      </p>

      <div className="mt-3 flex flex-wrap items-center justify-between gap-3">
        {/*
          Announced, so a council member who has just voted hears the tally move
          rather than only seeing their own button go quiet. Polite, because it
          is a running total and not an alert.
        */}
        <p
          className="text-sm font-medium"
          style={{ color: "var(--color-text)" }}
          aria-live="polite"
        >
          {tallySentence(proposal)}
        </p>

        {ownRemoval ? (
          <p className="text-sm" style={{ color: "var(--color-text-secondary)" }}>
            {blocked}
          </p>
        ) : (
          <div className="flex items-center gap-2">
            {voting && <Spinner size="sm" />}
            {voteButton("approve", "Approve", "var(--color-success)")}
            {voteButton("reject", "Decline", "var(--color-danger)")}
          </div>
        )}
      </div>

      {/*
        Why the buttons are disabled, for everything except the self-removal case
        the buttons themselves have already been replaced for.
      */}
      {blocked && !ownRemoval && (
        <p className="mt-2 text-xs" style={{ color: "var(--color-text-tertiary)" }}>
          {blocked}
        </p>
      )}
    </li>
  );
}
