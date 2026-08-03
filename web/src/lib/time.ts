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
 * formatAbsoluteTime renders the full timestamp used in hover tooltips.
 * Returns an empty string for an unparseable input so a bad value from the API
 * shows nothing rather than "Invalid Date".
 */
export function formatAbsoluteTime(dateStr: string): string {
  const date = new Date(dateStr);
  return Number.isNaN(date.getTime()) ? "" : date.toLocaleString();
}
