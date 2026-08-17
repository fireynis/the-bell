const SECOND = 1000;
const MINUTE = 60 * SECOND;
const HOUR = 60 * MINUTE;
const DAY = 24 * HOUR;

export interface RelativeTimeOptions {
  /** Current time in epoch milliseconds; injectable so tests need no fake clock. */
  now?: number;
  /**
   * Append " ago" to the short forms ("5m ago"). The moderation queue uses the
   * longer wording; the feed keeps the compact form to fit its dense layout.
   */
  suffix?: boolean;
}

/**
 * formatRelativeTime renders a timestamp as a short age ("just now", "5m",
 * "3h", "2d"), falling back to an absolute date once it is a week old.
 *
 * Timestamps in the future clamp to "just now" rather than showing a negative
 * age, which can happen from mild clock skew between the client and server.
 */
export function formatRelativeTime(dateStr: string, options: RelativeTimeOptions = {}): string {
  const { now = Date.now(), suffix = false } = options;

  const date = new Date(dateStr);
  const time = date.getTime();
  if (Number.isNaN(time)) return "";

  const ago = (value: string) => (suffix ? `${value} ago` : value);

  const diff = now - time;
  if (diff < MINUTE) return "just now";
  if (diff < HOUR) return ago(`${Math.floor(diff / MINUTE)}m`);
  if (diff < DAY) return ago(`${Math.floor(diff / HOUR)}h`);
  if (diff < 7 * DAY) return ago(`${Math.floor(diff / DAY)}d`);

  return date.toLocaleDateString(undefined, { month: "short", day: "numeric" });
}

/**
 * activeExpiry reads a timestamp that only means anything while it is still in
 * the future — a mute's end, a suspension's — and returns it, or null when
 * there is nothing in force at `now`.
 *
 * The comparison against the clock is the whole mechanism, mirroring
 * domain.User.IsMuted: nothing sweeps an expired restriction, it simply stops
 * applying, and a page left open outlives the response that loaded it.
 *
 * An unparseable timestamp reads as nothing in force rather than as an Invalid
 * Date, sharing the empty-string contract of the formatters below: a bad value
 * from the API leaves the plain view rather than a restriction nobody can name.
 */
export function activeExpiry(raw: string | null | undefined, now: Date): Date | null {
  if (!raw) return null;

  const expiry = new Date(raw);
  if (Number.isNaN(expiry.getTime())) return null;

  return expiry.getTime() > now.getTime() ? expiry : null;
}

/**
 * formatAbsoluteTime renders the full timestamp used in hover tooltips.
 * Returns an empty string for an unparseable input so a bad value from the API
 * shows nothing rather than "Invalid Date".
 */
export function formatAbsoluteTime(dateStr: string): string {
  const date = new Date(dateStr);
  return Number.isNaN(date.getTime()) ? "" : date.toLocaleString();
}

/**
 * formatMonthYear renders just the month and the year, for the directory's
 * "Joined March 2026".
 *
 * The day is deliberately dropped. How long somebody has been in town is the
 * useful fact when you are trying to place a name; which Tuesday they signed up
 * on is not, and printing it makes a list of neighbours read like an audit log.
 *
 * Shares the empty-string contract of the other two: an unparseable timestamp
 * shows nothing rather than the literal "Invalid Date", so the caller can drop
 * the line instead of rendering it broken.
 */
export function formatMonthYear(dateStr: string): string {
  const date = new Date(dateStr);
  if (Number.isNaN(date.getTime())) return "";

  return date.toLocaleDateString(undefined, { year: "numeric", month: "long" });
}

/**
 * formatDate renders the day, month and year, for the approval queue's "Joined
 * 3 March 2026".
 *
 * Between formatMonthYear and formatDateTime, and the queue is why: the day
 * matters when the council is deciding about somebody who has been waiting
 * twelve of them, but the minute they registered at does not, and printing it
 * makes a list of applicants read like a server log.
 *
 * Shares the empty-string contract of the others: an unparseable timestamp
 * shows nothing rather than "Invalid Date".
 */
export function formatDate(dateStr: string): string {
  const date = new Date(dateStr);
  if (Number.isNaN(date.getTime())) return "";

  return date.toLocaleDateString(undefined, { year: "numeric", month: "long", day: "numeric" });
}

/**
 * formatDateTime renders a date and time to the minute, for the moderation
 * audit trail where "3d" is not specific enough to defend a decision.
 *
 * Shares the empty-string contract of formatAbsoluteTime: a timestamp the API
 * sends malformed shows nothing rather than the literal "Invalid Date".
 */
export function formatDateTime(dateStr: string): string {
  const date = new Date(dateStr);
  if (Number.isNaN(date.getTime())) return "";

  return date.toLocaleDateString(undefined, {
    year: "numeric",
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}
