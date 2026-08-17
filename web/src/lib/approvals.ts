/**
 * How the town talks about the people waiting to be let into it.
 *
 * The approval queue is the one screen where a council member decides about a
 * stranger, and the wording is doing work there. These are applicants, not
 * tickets: the page counts neighbours rather than rows, and it says how long
 * somebody has been waiting rather than printing a date and leaving the
 * subtraction to the reader. A queue that reads like a work tray is a queue
 * people work through without looking up.
 *
 * The residency claim has its own module — see lib/residency — because it is a
 * privacy line rather than a matter of wording.
 */

/** A page of the queue. The same 25 as the directory, and the server's default. */
export const APPROVALS_PAGE_SIZE = 25;

/** How many of the longest-waiting the dashboard previews before saying "review all". */
export const APPROVALS_PREVIEW_SIZE = 3;

/** Nobody is waiting. Good news, said as good news. */
export const APPROVALS_EMPTY = "Nobody is waiting — the town is all caught up.";

/** Nobody matched the search. Different from an empty queue, so worded apart. */
export const APPROVALS_NO_MATCH = "Nobody waiting by that name.";

/** What the page says when the queue itself could not be read. */
export const APPROVALS_LOAD_ERROR = "We could not load the approval queue just now.";

/**
 * describeWaitingCount summarises how much of the queue is on screen.
 *
 * "Waiting" rather than "pending", and "neighbours" rather than "users": the
 * people in this list are the town's future residents, and the council member
 * reading it is deciding about a person. It says nothing at all when there is
 * nothing to count — the empty state has its own sentence and "0 neighbours"
 * underneath it would only repeat the bad news.
 *
 * Mirrors describeNeighborCount in lib/directory, deliberately: the two lists
 * are read the same way and a count that phrased itself differently on each
 * would be an arbitrary difference to notice.
 */
export function describeWaitingCount(
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
      ? "neighbour waiting"
      : "neighbours waiting";

  return shown < total ? `Showing ${shown} of ${total} ${noun}` : `${total} ${noun}`;
}

/**
 * describeQueueSize is the dashboard's one-line version: "7 neighbours
 * waiting", with no page to compare against because the preview is not a page.
 *
 * Returns the empty string for nobody waiting, so the dashboard renders nothing
 * rather than announcing an empty queue every time a council member opens the
 * Town Hall.
 */
export function describeQueueSize(total: number | null): string {
  if (total === null || !Number.isFinite(total) || total <= 0) return "";
  return total === 1 ? "1 neighbour waiting" : `${total} neighbours waiting`;
}

const MINUTE = 60 * 1000;
const HOUR = 60 * MINUTE;
const DAY = 24 * HOUR;

/**
 * describeWaitingTime says how long somebody has been waiting to be let in.
 *
 * The queue is ordered by exactly this, so the elapsed time is the thing the
 * council is being asked to act on — and "Waiting 12 days" is a prompt in a way
 * that "Joined 1 August" is not, even though they are the same fact. The date
 * is still shown beside it; this replaces the arithmetic, not the record.
 *
 * Rounded down to the coarsest honest unit, because precision here is false
 * comfort: nobody decides differently at 13 hours than at 14. Anything under an
 * hour reads as "just now" rather than as a count of minutes, so a page left
 * open does not tick.
 *
 * A timestamp in the future clamps to "just now" rather than showing a negative
 * wait, which mild clock skew between client and server produces. An
 * unparseable one returns the empty string, sharing lib/time's contract, so the
 * caller drops the line rather than rendering "Waiting NaN days".
 */
export function describeWaitingTime(joinedAt: string | null | undefined, now: number): string {
  if (!joinedAt) return "";

  const joined = new Date(joinedAt).getTime();
  if (Number.isNaN(joined)) return "";

  const elapsed = now - joined;
  if (elapsed < HOUR) return "Waiting since just now";
  if (elapsed < DAY) {
    const hours = Math.floor(elapsed / HOUR);
    return `Waiting ${hours} ${hours === 1 ? "hour" : "hours"}`;
  }

  const days = Math.floor(elapsed / DAY);
  return `Waiting ${days} ${days === 1 ? "day" : "days"}`;
}
