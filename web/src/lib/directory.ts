import type { Role } from "./trust";

/**
 * How the member directory talks about the people in it.
 *
 * The directory is where somebody goes looking for a neighbour they know, so
 * its job is discovery rather than administration — which is why the role words
 * here are not the ones RoleBadge shows on a profile. "Pending" is accurate and
 * it is also the word for a queue; on a page whose whole purpose is that these
 * are people you might recognise, a newcomer reads better as somebody to greet
 * than as somebody awaiting processing.
 */

/**
 * The invitation shown to a member the town has not rung in yet.
 *
 * It sits here rather than in the page for the same reason PENDING_WELCOME sits
 * in lib/gating: what a pending member is told is a policy question, not a
 * layout one. Vouching itself happens on a profile, so this points them at the
 * one action available from here — going and looking at somebody.
 */
export const NEIGHBORS_INVITE = {
  title: "These are your neighbours.",
  body: "Any member can ring you in by vouching for you — if you know someone here, pay them a visit.",
} as const;

/** The warm reading of each role, for the directory only. */
const ROLE_LABELS: Partial<Record<Role, string>> = {
  pending: "New here",
  member: "Member",
  moderator: "Moderator",
  council: "Council",
  banned: "Banned",
};

/**
 * roleLabel names a role as the directory says it.
 *
 * A role this build has never heard of is capitalised and shown as it came,
 * rather than dropped or guessed at: the server can introduce a role at any
 * time, and showing an unfamiliar word is honest where showing nothing is not.
 * An empty role yields an empty string so the caller can leave the line off
 * entirely rather than render a stray separator.
 */
export function roleLabel(role: string | null | undefined): string {
  const trimmed = (role ?? "").trim();
  if (!trimmed) return "";

  const known = ROLE_LABELS[trimmed as Role];
  if (known) return known;

  return trimmed.charAt(0).toUpperCase() + trimmed.slice(1);
}

/**
 * describeNeighborCount summarises how much of the roll is on screen.
 *
 * The total comes from the server and counts everyone who matches, not everyone
 * loaded — so it is the only way to tell "these are all of them" from "this is
 * the first page of them", which is the question somebody scanning for a name
 * actually has. It says nothing at all when there is nothing to count: an empty
 * list has its own message, and "0 neighbours" underneath it would only repeat
 * the bad news.
 */
export function describeNeighborCount(
  shown: number,
  total: number | null,
  searching: boolean,
): string {
  if (total === null || !Number.isFinite(total) || total <= 0) return "";

  const noun = searching
    ? total === 1
      ? "match"
      : "matches"
    : total === 1
      ? "neighbour"
      : "neighbours";

  return shown < total ? `Showing ${shown} of ${total} ${noun}` : `${total} ${noun}`;
}
