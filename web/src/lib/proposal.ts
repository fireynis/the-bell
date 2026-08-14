import type { ApiError, DirectoryUser, Proposal, ProposalType } from "../api/types";
import { personName } from "./people";
import { runeLength, validationDetail, type ValidationResult } from "./post";
import { formatDateTime } from "./time";
import { UNVERIFIED_EMAIL_NOTICE, isEmailUnverified } from "./verification";

/**
 * How the town hall talks about what the council is deciding.
 *
 * Every rule encoded here belongs to the server; this side only mirrors it so
 * the page can say what will happen and stop offering a vote that would be
 * refused. Each mirror names the rule it copies, because a mirror nobody can
 * trace is worse than none — it looks authoritative and drifts silently.
 *
 * The rules, in the server's words:
 *
 *   - only the council may see or vote on proposals — the route guard, which is
 *     also why the page itself is council-gated;
 *   - nobody votes twice, so a cast vote is final;
 *   - a removal's target does not vote on their own removal, and is excluded
 *     from that proposal's electorate — which is why council_size travels with
 *     each proposal instead of being counted once for the page;
 *   - a promotion or a removal that reaches a majority is carried out
 *     immediately, in the same request as the deciding vote.
 */

/** Mirrors the rationale bound in the create-proposal contract: 1..1000 characters. */
export const MAX_RATIONALE_LENGTH = 1000;

/**
 * Mirrors bootstrapExitThreshold in internal/service/approval.go.
 *
 * It is quoted in the consequence line for a bootstrap re-entry, because "until
 * the town is big enough again" is not something a council member can weigh and
 * "until the town reaches 20 members" is.
 */
export const BOOTSTRAP_EXIT_THRESHOLD = 20;

/**
 * The three kinds of proposal, with the words the create form offers them in.
 *
 * Ordered by how often a council will reach for one. The wording is a verb
 * phrase rather than the wire's snake_case, so the select reads as a list of
 * things to do rather than a list of enum members.
 */
export const PROPOSAL_TYPE_OPTIONS: readonly { value: ProposalType; label: string }[] = [
  { value: "council_promotion", label: "Promote a moderator to the council" },
  { value: "council_removal", label: "Remove someone from the council" },
  { value: "bootstrap_reentry", label: "Reopen the town to council approvals" },
];

/**
 * proposalTitle says in plain words what is being proposed.
 *
 * A kind this build does not recognise gets a sentence rather than a blank or
 * the raw wire value: the server may grow a fourth kind, and a council member
 * meeting one should see that there is something to decide and go and read the
 * rationale, not a row that looks broken.
 */
export function proposalTitle(
  proposal: Pick<Proposal, "type" | "target_user_id" | "target_display_name">,
): string {
  const who = personName(proposal.target_display_name, proposal.target_user_id);

  switch (proposal.type) {
    case "council_promotion":
      return `Promote ${who} to council`;
    case "council_removal":
      return `Remove ${who} from council`;
    case "bootstrap_reentry":
      return "Reopen the town to council approvals";
    default:
      return "A proposal this version of the app does not recognise";
  }
}

/**
 * proposalConsequence spells out what passing actually does.
 *
 * It is on the card rather than in a help page because the consequence is the
 * whole decision: a promotion and a removal take effect in the same request as
 * the deciding vote, with no confirmation step behind which to reconsider, and a
 * council member is entitled to know that before they click rather than after.
 */
export function proposalConsequence(type: string): string {
  switch (type) {
    case "council_promotion":
      return "If more than half the council approves, they join the council straight away — there is no further step.";
    case "council_removal":
      return "If more than half the council approves, they lose their council seat straight away. They do not vote on this.";
    case "bootstrap_reentry":
      return `If more than half the council approves, the town reopens to council approvals until it reaches ${BOOTSTRAP_EXIT_THRESHOLD} members again.`;
    default:
      return "Passing this proposal has an effect this version of the app cannot describe.";
  }
}

/**
 * What the town hall says when the council has nothing open.
 *
 * A question rather than a full stop: an empty town hall is not an error state,
 * it is an invitation, and the section keeps its "raise a proposal" control
 * beside this line so the invitation can be acted on.
 */
export const NO_OPEN_PROPOSALS =
  "No open proposals. Anything the council should decide together?";

/**
 * noCandidatesMessage explains an empty person picker, which is otherwise
 * indistinguishable from one that failed to load.
 *
 * Both cases are real and neither is a fault: a town with no moderators has
 * nobody to promote, and a council of one has nobody but themselves to remove.
 */
export function noCandidatesMessage(type: ProposalType): string {
  return type === "council_promotion"
    ? "There are no moderators to promote yet."
    : "There is nobody on the council to remove.";
}

/** needsTarget reports whether a kind of proposal is about a person. */
export function needsTarget(type: ProposalType): boolean {
  return type === "council_promotion" || type === "council_removal";
}

/**
 * eligibleCandidates narrows the directory to the people a proposal of this kind
 * can name: moderators to promote, council members to remove.
 *
 * Filtered on the client because the directory has no role filter, and the two
 * groups are small — a town has a handful of moderators and fewer council
 * members. The server decides eligibility for real; this only keeps the picker
 * from listing three hundred neighbours for a choice with four answers.
 */
export function eligibleCandidates(
  users: readonly DirectoryUser[],
  type: ProposalType,
): DirectoryUser[] {
  if (!needsTarget(type)) return [];

  const wanted = type === "council_promotion" ? "moderator" : "council";
  return (users ?? []).filter((u) => u?.role === wanted);
}

/**
 * tallySentence reports where the vote stands, in one line a council member can
 * read at a glance.
 *
 * The denominator is the proposal's own council_size, never a count taken from
 * the page: a removal excludes its target from the electorate, so the same
 * council can be five for one proposal on screen and four for the one under it.
 *
 * "In favour" is stated separately from the turnout because they answer
 * different questions — how close this is to being decided, and which way it is
 * going.
 */
export function tallySentence(
  proposal: Pick<Proposal, "approve_count" | "reject_count" | "council_size">,
): string {
  const approvals = safeCount(proposal?.approve_count);
  const voted = approvals + safeCount(proposal?.reject_count);
  const council = safeCount(proposal?.council_size);

  if (voted === 0) return "Nobody has voted yet.";

  const verb = voted === 1 ? "has" : "have";
  const favour = `${approvals} in favour`;

  // A council of nobody is not a state the server should produce, but a tally
  // out of zero would read as "2 of 0 council members", which is worse than
  // simply not naming a denominator nobody can trust.
  if (council <= 0) {
    return `${voted} ${voted === 1 ? "vote" : "votes"} cast — ${favour}.`;
  }

  return `${voted} of ${council} council members ${verb} voted — ${favour}.`;
}

/** Reads a count off the wire, treating anything malformed as zero. */
function safeCount(value: number | undefined): number {
  return Number.isFinite(value) ? Math.max(0, Math.trunc(value as number)) : 0;
}

/**
 * majorityOf reports how many approvals carry a proposal of this size — more
 * than half, so three of five and three of four.
 */
export function majorityOf(councilSize: number): number {
  const council = safeCount(councilSize);
  return council <= 0 ? 0 : Math.floor(council / 2) + 1;
}

/**
 * voteBlockReason explains why this council member has no vote to cast on a
 * proposal, or returns null when they do.
 *
 * The self-removal case is first and worded for the person reading it. Somebody
 * whose seat is being voted on can see the proposal — hiding it would be worse —
 * but they are not part of its electorate, and a bare disabled button would look
 * like the page had failed rather than like a rule.
 */
export function voteBlockReason(
  proposal: Pick<Proposal, "type" | "target_user_id" | "status" | "my_vote">,
  viewerId: string | null | undefined,
): string | null {
  if (!proposal) return "This proposal is no longer available.";

  if (
    proposal.type === "council_removal" &&
    viewerId &&
    proposal.target_user_id === viewerId
  ) {
    return "You don't vote on your own removal.";
  }

  if (proposal.status !== "open") {
    return "This proposal has already been decided.";
  }

  // The server refuses a second vote outright; there is nothing to change your
  // mind with, so the page says so rather than offering a button that 409s.
  if (proposal.my_vote === "approve") return "You voted in favour.";
  if (proposal.my_vote === "reject") return "You voted against.";

  return null;
}

/** canVoteOn is the boolean half of voteBlockReason, for gating the buttons. */
export function canVoteOn(
  proposal: Pick<Proposal, "type" | "target_user_id" | "status" | "my_vote">,
  viewerId: string | null | undefined,
): boolean {
  return voteBlockReason(proposal, viewerId) === null;
}

/**
 * isOwnRemoval reports whether this proposal is about unseating the person
 * reading it, which is the one case where a proposal is shown with no vote
 * controls at all rather than with disabled ones.
 */
export function isOwnRemoval(
  proposal: Pick<Proposal, "type" | "target_user_id">,
  viewerId: string | null | undefined,
): boolean {
  return (
    proposal?.type === "council_removal" && !!viewerId && proposal.target_user_id === viewerId
  );
}

/**
 * validateRationale applies the server's 1..1000 bound so the form can refuse
 * an empty rationale rather than round-trip to a 400.
 *
 * Measured in characters with runeLength, because the contract is written in
 * characters — measuring bytes would refuse a rationale in any language that
 * does not fit in ASCII long before the server would.
 *
 * Emptiness is judged after trimming: whitespace is not a reason.
 */
export function validateRationale(rationale: string): ValidationResult {
  const trimmed = (rationale ?? "").trim();
  if (trimmed.length === 0) {
    return { valid: false, error: "Say why the council should do this." };
  }

  const chars = runeLength(trimmed);
  if (chars > MAX_RATIONALE_LENGTH) {
    return {
      valid: false,
      error: `That is ${chars} characters; the maximum is ${MAX_RATIONALE_LENGTH}.`,
    };
  }

  return { valid: true };
}

/** openProposals selects the ones still awaiting the council's decision. */
export function openProposals(proposals: readonly Proposal[]): Proposal[] {
  return (proposals ?? []).filter((p) => p?.status === "open");
}

/** decidedProposals selects the ones that are now history, in the order given. */
export function decidedProposals(proposals: readonly Proposal[]): Proposal[] {
  return (proposals ?? []).filter((p) => p && p.status !== "open");
}

/**
 * applyProposalUpdate folds a proposal the server has just returned back into
 * the list on screen.
 *
 * Replaced in place rather than removed, because a vote that decides a proposal
 * does not make it vanish: it moves from the open list to the history below it,
 * and the caller derives both lists from this one array. That is also why this
 * does not filter — a vote that passed a removal must leave visible evidence
 * that it passed, not silently take the card away.
 *
 * A proposal that is not already in the list is not inserted. The list is what
 * this council member was shown, and an update about something outside it is not
 * a reason to grow it.
 */
export function applyProposalUpdate(
  proposals: readonly Proposal[],
  updated: Proposal | null | undefined,
): Proposal[] {
  const current = [...(proposals ?? [])];
  if (!updated || typeof updated.id !== "string") return current;

  return current.map((p) => (p?.id === updated.id ? updated : p));
}

/**
 * decidedSummary is the one line the history list shows: what the council
 * decided, and when.
 *
 * The date is dropped rather than guessed at when the server sends none — an
 * "Invalid Date" beside a decision is worse than a decision with no date, and
 * formatDateTime already answers with the empty string for anything unparseable.
 */
export function decidedSummary(proposal: Pick<Proposal, "status" | "decided_at">): string {
  const outcome = proposal?.status === "passed" ? "Passed" : "Rejected";
  const when = formatDateTime(proposal?.decided_at ?? "");
  return when ? `${outcome} ${when}` : outcome;
}

/**
 * proposalErrorMessage turns a refused vote or a refused proposal into a
 * sentence.
 *
 * The 409 is the one worth naming: it is what a second vote and a proposal
 * decided since the page loaded both come back as, and in either case the fix is
 * to reload rather than to try again.
 */
export function proposalErrorMessage(
  err: ApiError | null | undefined,
  action: "vote" | "create",
): string {
  const fallback =
    action === "vote"
      ? "Your vote could not be recorded. Please try again."
      : "The proposal could not be raised. Please try again.";

  if (!err) return fallback;

  if (isEmailUnverified(err)) return UNVERIFIED_EMAIL_NOTICE;

  switch (err.status) {
    case 409:
      return "The council has already settled this one. Reload to see where it landed.";
    case 404:
      return "That proposal is no longer available.";
    case 401:
    case 403:
      return "Only the council can do that.";
    case 400:
      return validationDetail(err.error) ?? fallback;
    default:
      return fallback;
  }
}
