import type { OwnModerationEntry } from "../api/types";
import { severityConsequence } from "./moderation";
import { formatDateTime, formatMonthYear } from "./time";

/**
 * Turning a member's own moderation record into plain sentences.
 *
 * This is the member's side of the audit trail, and it reads nothing like the
 * moderator's. ActionHistoryCard shows a coloured severity badge and the words
 * "Severity: 3" to somebody deciding what to do about a stranger; the person on
 * the receiving end needs to know what happened to them, why, when they can
 * post again, and when the cost stops counting against them. Neither jargon nor
 * a scolding tone belongs in that.
 *
 * Nothing here names a moderator, because nothing the server sends does.
 */

/** What the member is told about one action, sentence by sentence. */
export interface OwnActionSummary {
  /** "You were warned — minor", "You were muted". */
  headline: string;
  /** The moderator's reason, exactly as they wrote it. */
  reason: string;
  /** When it happened: "Mar 1, 2026, 12:00 PM". */
  when: string;
  /**
   * When the restriction ends or ended, or null for an action that does not
   * expire — a warning and a ban.
   */
  restriction: string | null;
  /**
   * What it cost in trust and when that fades, or null when no penalty was
   * recorded against the member for this action.
   */
  cost: string | null;
}

/**
 * The severity word a warning carries, in the member's own terms.
 *
 * Deliberately only the two a warning can take. Every other action type has
 * exactly one severity — a mute is always 3, a suspension 4, a ban 5 — so
 * naming it would repeat the action rather than tell the member anything. That
 * is the same rule actionNoun in ./moderation.ts follows on the moderator's
 * side, where the severity is named for a warning and for nothing else.
 *
 * The words mirror SEVERITY_NAMES in ./moderation.ts, which is private to that
 * module. They are stated again rather than shared because the two audiences
 * differ: a moderator picking a severity reads "Ban-level", and that is not a
 * phrase to show a member about themselves.
 */
const WARN_SEVERITY_WORDS: Record<number, string> = {
  1: "minor",
  2: "moderate",
};

/**
 * How each action type opens the entry. Written in the passive, addressed to
 * the member: "You were muted", not "Mute (severity 3)".
 */
const ACTION_HEADLINES: Record<string, string> = {
  warn: "You were warned",
  mute: "You were muted",
  suspend: "You were suspended",
  ban: "You were banned",
};

/**
 * What to say when the server sends an action type this build has never heard
 * of. The record still gets shown — a member must not be kept from a real
 * moderation decision because the frontend is a version behind — it is simply
 * described in the vaguest terms that are still true.
 */
const UNKNOWN_ACTION_HEADLINE = "A moderator acted on your account";

/**
 * Points as a member should read them: 5, not 5.000000000000001. Direct
 * penalties are whole numbers today (5, 10, 25, 40, 100), so this only ever
 * matters if that policy grows a fractional value.
 */
function formatPoints(points: number): string {
  const rounded = Math.round(points * 10) / 10;
  return Number.isInteger(rounded) ? String(rounded) : rounded.toFixed(1);
}

function headlineFor(entry: OwnModerationEntry): string {
  const base = ACTION_HEADLINES[entry.action];
  if (!base) return UNKNOWN_ACTION_HEADLINE;

  const severityWord = entry.action === "warn" ? WARN_SEVERITY_WORDS[entry.severity] : undefined;
  return severityWord ? `${base} — ${severityWord}` : base;
}

/**
 * When the restriction ends, in the tense that matches the clock. A mute the
 * member is still serving reads "This ends on…"; one that has run out reads
 * "This ended on…", so nobody is left believing they still cannot post.
 *
 * An unparseable or absent timestamp yields no line at all rather than
 * "Invalid Date", matching formatDateTime's contract.
 */
function restrictionFor(entry: OwnModerationEntry, now: Date): string | null {
  if (!entry.expires_at) return null;

  const formatted = formatDateTime(entry.expires_at);
  if (!formatted) return null;

  const ends = new Date(entry.expires_at);
  return ends.getTime() > now.getTime()
    ? `This ends on ${formatted}.`
    : `This ended on ${formatted}.`;
}

/**
 * What the action cost, and when the cost stops counting.
 *
 * The fade date is the useful half. A trust penalty is not a mark on a record
 * that sits there forever — most of them decay — and a member who cannot see
 * that has no way to know their standing will recover.
 *
 * "They do not fade" is claimed only when the severity itself says so, checked
 * against severityConsequence rather than inferred from a missing date. A date
 * absent because the server omitted it and a date absent because the penalty is
 * permanent look identical on the wire; only the severity distinguishes them,
 * and telling somebody a penalty is permanent when it is not would be the worse
 * of the two mistakes.
 */
function costFor(entry: OwnModerationEntry): string | null {
  if (!entry.penalty) return null;

  const points = formatPoints(entry.penalty.amount);
  const faded = entry.penalty.decays_at ? formatMonthYear(entry.penalty.decays_at) : "";
  if (faded) {
    return `This cost ${points} trust points; fully faded by ${faded}.`;
  }

  const consequence = severityConsequence(entry.severity);
  if (consequence?.decayDays === null) {
    return `This cost ${points} trust points, and they do not fade.`;
  }
  return `This cost ${points} trust points.`;
}

/**
 * describeOwnAction turns one entry into the sentences shown to the member it
 * concerns.
 *
 * `now` is a parameter so the tense of the restriction line is testable without
 * a fake clock, matching ownMuteNotice in ./moderation.ts.
 */
export function describeOwnAction(entry: OwnModerationEntry, now: Date): OwnActionSummary {
  return {
    headline: headlineFor(entry),
    reason: entry.reason ?? "",
    when: formatDateTime(entry.created_at),
    restriction: restrictionFor(entry, now),
    cost: costFor(entry),
  };
}

/**
 * What the section says to the overwhelming majority of members, who will open
 * it once out of curiosity and never again.
 *
 * The second clause is the point. An empty moderation history is not a blank
 * screen to apologise for; it is the normal state of an account, and saying so
 * keeps the section from reading like a warning that something is pending.
 */
export const OWN_HISTORY_EMPTY = "Nothing here — and that's how it stays for most people.";
