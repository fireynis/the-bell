import type { ApiError, Invite, RegistrationMode, TownConfig, User } from "../api/types";
import { validationDetail, runeLength, type ValidationResult } from "./post";
import { formatDate } from "./time";
import { canVouch, isCouncil, VOUCHING_THRESHOLD } from "./trust";
import { DAILY_VOUCH_LIMIT } from "./vouch";
import { UNVERIFIED_EMAIL_NOTICE, isEmailUnverified } from "./verification";

/**
 * Invitations, and the one sentence that explains all of them: the invitation
 * is the vouch.
 *
 * Nothing here is a second way into the town. An invitation costs its sender a
 * vouch out of the same daily three, and when the newcomer accepts it the vouch
 * is created and they arrive as a member rather than as somebody waiting to be
 * recognised. That is why the gate below is canVouch and not a new permission,
 * and why the dialog says out loud what the sender is staking.
 */

/** Mirrors maxInviteNoteRunes in internal/service/invite.go. */
export const MAX_INVITE_NOTE_LENGTH = 500;

/** The cookie the Kratos proxy reads the raw token out of during registration. */
export const INVITE_COOKIE_NAME = "bell_invite";

/**
 * An hour, matching the cookie the server expects. Long enough to read a
 * greeting, pick a password and fix the two mistakes a registration form finds;
 * short enough that a token does not sit in a shared browser for a day.
 */
export const INVITE_COOKIE_MAX_AGE_SECONDS = 3600;

const DAY_MS = 24 * 60 * 60 * 1000;

/** The subset of the signed-in member the invite gate reads. */
type Inviter = Pick<User, "trust_score" | "role" | "is_active"> | null;

/**
 * canInvite mirrors the gate on POST /v1/invites: whoever may vouch may invite,
 * and council may invite whatever their score.
 *
 * It is canVouch and not a threshold of its own because an invitation *is* a
 * vouch — accepting one creates the edge. Anything that let somebody invite
 * who could not vouch would be a way of vouching around the vouching rule.
 *
 * The council exemption is theirs alone and is about the town rather than the
 * person: a council member is how a new town gets its first residents, and on
 * day one nobody's score is anywhere near {@link VOUCHING_THRESHOLD}.
 */
export function canInvite(user: Inviter): boolean {
  return canVouch(user) || isCouncil(user);
}

/**
 * registrationMode reads how the town admits newcomers off the public config.
 *
 * An absent or unrecognised value reads as "open", which is both the older
 * behaviour and the safer failure: a town that cannot say how it admits people
 * should not have its front door bolted by a config read that did not land.
 * Getting this wrong in the other direction locks every would-be resident out
 * of a town that never asked for invitations. The server is the authority
 * either way — the proxy refuses an uninvited registration in invite mode
 * whatever this returns.
 */
export function registrationMode(config: TownConfig | null | undefined): RegistrationMode {
  return config?.registration_mode === "invite" ? "invite" : "open";
}

/**
 * setInviteCookie hands the raw token to the server for the registration flow.
 *
 * The token travels in a cookie rather than in the request because the browser
 * talks to Kratos through our own proxy and never touches the registration
 * payload — the proxy is what reads this, on the flow paths, and refuses a
 * registration whose email does not match the invitation's.
 *
 * Not Secure: the SPA and the proxy are the same origin, so the cookie never
 * leaves it, and marking it Secure would break every local http deployment.
 * SameSite=Lax because the person arrives by following a link from their email.
 *
 * `doc` is injected so a test can assert the cookie was written before the flow
 * was fetched — which is the whole ordering requirement, and invisible from the
 * outside if it is wrong.
 */
export function setInviteCookie(token: string, doc: Pick<Document, "cookie"> = document): void {
  const value = encodeURIComponent(token);
  doc.cookie = `${INVITE_COOKIE_NAME}=${value}; Path=/; SameSite=Lax; Max-Age=${INVITE_COOKIE_MAX_AGE_SECONDS}`;
}

/**
 * validateInviteEmail refuses only what is certainly not an address.
 *
 * Deliberately looser than a full grammar. The server parses with
 * net/mail.ParseAddress, which accepts more than any regular expression people
 * write from memory — a client stricter than the server refuses addresses that
 * would have worked, and the person on the other end never finds out why. So
 * this catches the blank field and the missing @, and leaves the ruling to the
 * server, whose refusal is passed through verbatim.
 */
export function validateInviteEmail(email: string): ValidationResult {
  const trimmed = (email ?? "").trim();
  if (trimmed.length === 0) {
    return { valid: false, error: "Who are you inviting? Enter their email address." };
  }

  const at = trimmed.indexOf("@");
  const looksLikeAddress =
    at > 0 && at === trimmed.lastIndexOf("@") && at < trimmed.length - 1 && !/\s/.test(trimmed);
  if (!looksLikeAddress) {
    return { valid: false, error: "That does not look like an email address." };
  }

  return { valid: true };
}

/**
 * validateInviteNote bounds the personal note at the server's 500 runes.
 *
 * Runes rather than bytes, matching the server's utf8.RuneCountInString — see
 * runeLength for why the difference matters. An empty note is valid: the note
 * is an offer, not a requirement, and an invitation with nothing written on it
 * is a perfectly ordinary invitation.
 */
export function validateInviteNote(note: string): ValidationResult {
  const runes = runeLength((note ?? "").trim());
  if (runes > MAX_INVITE_NOTE_LENGTH) {
    return {
      valid: false,
      error: `Your note is ${runes} characters; the maximum is ${MAX_INVITE_NOTE_LENGTH}.`,
    };
  }
  return { valid: true };
}

/** remainingNoteChars reports how much of the note is left, going negative when over. */
export function remainingNoteChars(note: string): number {
  return MAX_INVITE_NOTE_LENGTH - runeLength(note ?? "");
}

/**
 * What the sender is told they are doing, before they do it.
 *
 * The number is spelled out because the sentence is prose, and it is the same
 * number as DAILY_VOUCH_LIMIT — the budget really is shared, an invitation
 * spends a vouch. The test beside this pins the two together so the sentence
 * cannot go on saying "three" after the limit becomes something else.
 */
export const INVITE_CONSEQUENCE =
  "Inviting someone is vouching for them — when they arrive, you'll have staked some of " +
  "your standing on them, and it counts against today's three vouches.";

/** How the invite dialog introduces itself. */
export const INVITE_DIALOG = {
  title: "Invite a neighbour",
  emailLabel: "Their email address",
  emailPlaceholder: "neighbour@example.com",
  noteLabel: "A note, if you like",
  noteHelp: "They will read this in the invitation. A line about how you know them is plenty.",
  notePlaceholder: "We met at the market...",
  submit: "Send the invitation",
  sending: "Sending...",
  /** The heading over the link, once the invitation exists. */
  readyTitle: "The invitation is ready",
  linkLabel: "Their invitation link",
  copy: "Copy link",
  /** The button's own label once it has worked, so the click has an answer in place. */
  copied: "Copied",
  /** The live region's version, worded apart so a screen reader is not read the same word twice. */
  copiedNotice: "Copied — the link is on your clipboard.",
  /** Shown when the clipboard is unavailable and the link has been selected instead. */
  selected: "Selected — press Ctrl+C to copy.",
  emailSent: "An invitation is on its way to their inbox.",
  emailFailed: "The email couldn't be sent — share this link with them yourself.",
  done: "Done",
} as const;

/** How the sender's own list of invitations talks about itself. */
export const INVITES_SECTION = {
  title: "Your invitations",
  empty: "Nobody invited yet — is there a neighbour missing from this page?",
  loadError: "We could not load your invitations just now.",
  revoke: "Revoke",
  revokeTitle: "Withdraw this invitation?",
  /** Read by the confirm dialog; names the address so nobody revokes the wrong one. */
  revokeBody: (email: string) =>
    `The link sent to ${email} will stop working. Nothing is held against them, and you can ` +
    "invite them again whenever you like.",
} as const;

/**
 * describeInviteCount summarises the sender's invitations in the collapsed
 * header, so opening the section is a choice rather than the only way to find
 * out whether there is anything in it.
 *
 * It counts the ones still out — what is open is the only part of the list that
 * can still change, and the only part worth a glance. Says nothing at all when
 * there is nothing out, so a member with an old accepted invitation does not
 * get "0 waiting" reported at them every time they open the page.
 */
export function describeInviteCount(invites: readonly Invite[] | null | undefined): string {
  if (!Array.isArray(invites)) return "";
  const open = invites.filter((i) => i?.status === "open").length;
  if (open <= 0) return "";
  return open === 1 ? "1 still waiting" : `${open} still waiting`;
}

/**
 * describeInviteExpiry says how long an open invitation has left.
 *
 * Counted in whole days from now, rounded up, because "expires in 5 days" is
 * how someone decides whether to nudge their neighbour and an hours-and-minutes
 * countdown on a fourteen-day window is false precision. The last day reads as
 * "today" rather than "in 0 days".
 *
 * An expiry already behind the clock reads as expired even on an invitation the
 * server still calls open: nothing sweeps them, the status is only as fresh as
 * the response that carried it, and a page left open overnight must not promise
 * a link that has stopped working. An unparseable or missing timestamp returns
 * the empty string, sharing lib/time's contract, so the caller can drop the
 * line rather than render "Expires in NaN days".
 */
export function describeInviteExpiry(
  expiresAt: string | null | undefined,
  now: number = Date.now(),
): string {
  if (!expiresAt) return "";

  const expiry = new Date(expiresAt).getTime();
  if (Number.isNaN(expiry)) return "";

  const remaining = expiry - now;
  if (remaining <= 0) return "Expired";
  if (remaining <= DAY_MS) return "Expires today";
  if (remaining <= 2 * DAY_MS) return "Expires tomorrow";

  return `Expires in ${Math.ceil(remaining / DAY_MS)} days`;
}

/**
 * describeInviteStatus is the one line under an invitation's address saying
 * where it stands.
 *
 * Each status is a different kind of fact, so each gets its own sentence rather
 * than a shared word and a date. An accepted invitation names who arrived,
 * because that is the point of having sent it — and falls back to saying they
 * arrived when the newcomer has set no display name yet, which is the common
 * case on the day they join.
 *
 * A status this build has never heard of is capitalised and shown as it came,
 * for the reason roleLabel does the same: the server can learn a fifth one, and
 * showing an unfamiliar word is honest where showing nothing is not.
 */
export function describeInviteStatus(invite: Invite, now: number = Date.now()): string {
  switch (invite?.status) {
    case "open":
      return describeInviteExpiry(invite.expires_at, now) || "Waiting to be answered";
    case "accepted": {
      const name = invite.consumed_by_display_name?.trim();
      return name ? `Accepted — they joined as ${name}` : "Accepted — they have joined";
    }
    case "expired": {
      const on = formatDate(invite.expires_at);
      return on ? `Expired ${on} — nobody used it` : "Expired — nobody used it";
    }
    case "revoked":
      return "Revoked — you withdrew this one";
    default: {
      // The union is exhausted above, so this narrows to `never` — but the wire
      // is not typed, and a server that has learned a fifth status sends it
      // here. The cast is what lets the unfamiliar word be shown rather than
      // swallowed by a type that promised it could not arrive.
      const raw = ((invite as { status?: string })?.status ?? "").trim();
      return raw ? raw.charAt(0).toUpperCase() + raw.slice(1) : "";
    }
  }
}

/**
 * canRevokeInvite mirrors the server's rule on DELETE /v1/invites/{id}: an
 * invitation can be withdrawn while it is still open, and only by whoever sent
 * it — which the endpoint enforces by only ever showing a caller their own.
 *
 * Accepted is the one that matters here. Revoking it would suggest the vouch
 * behind it could be taken back from this list, and it cannot: once somebody
 * has arrived, withdrawing the endorsement is a revoke on the vouch itself,
 * with the penalty that carries.
 */
export function canRevokeInvite(invite: Pick<Invite, "status"> | null | undefined): boolean {
  return invite?.status === "open";
}

/**
 * inviteErrorMessage turns a refused invitation into a sentence the sender can
 * act on.
 *
 * The server's own wording carries the interesting half. Already invited, a
 * malformed address and the shared daily budget all arrive as validation
 * errors, and its sentence distinguishes them better than any guess made from a
 * status code — so the 400 and 409 branches pass it through, exactly as
 * reportErrorMessage does, and only fall back to a generic line when there is
 * nothing usable in it.
 *
 * The 403 is worth its own sentence: the button is only offered to somebody who
 * could vouch, so meeting this means their standing changed underneath them
 * while the page was open, and naming the threshold is what makes that legible
 * rather than mysterious.
 */
export function inviteErrorMessage(
  err: ApiError | null | undefined,
  action: "send" | "revoke" = "send",
): string {
  const fallback =
    action === "send"
      ? "The invitation could not be sent. Please try again."
      : "The invitation could not be withdrawn. Please try again.";

  if (!err) return fallback;

  // Ahead of the switch: the verification guard answers 403 too, and being told
  // your trust is too low would send somebody chasing vouches when all that is
  // in the way is an unopened email.
  if (isEmailUnverified(err)) return UNVERIFIED_EMAIL_NOTICE;

  switch (err.status) {
    case 429:
      return (
        `Invitations and vouches share a budget of ${DAILY_VOUCH_LIMIT} a day, and today's are ` +
        "spent. You can invite someone again tomorrow."
      );
    case 409:
      return (
        validationDetail(err.error) ??
        "There is already an invitation out to that address. It will lapse on its own if they never use it."
      );
    case 404:
      return action === "revoke"
        ? "That invitation is no longer there — it may have been accepted already."
        : fallback;
    case 401:
    case 403:
      return action === "revoke"
        ? "That invitation is not yours to withdraw."
        : `You need a trust score of ${VOUCHING_THRESHOLD} to invite someone. Trust grows when other members vouch for you.`;
    case 400:
      return validationDetail(err.error) ?? fallback;
    default:
      return fallback;
  }
}

/**
 * What somebody who arrives at registration with no usable invitation is told.
 *
 * It is an explanation rather than a refusal, and it has to be: the person
 * reading it did nothing wrong, and there is exactly one thing to do about it —
 * find somebody in town and ask. A bare "registration is closed" would read as
 * the town being shut, when in fact the door opens for anybody who knows a
 * neighbour.
 */
export const INVITE_ONLY_NOTICE = {
  title: "The Bell is invitation-only",
  /** Takes the town's name so the explanation is about somewhere, not about software. */
  body: (townName: string) =>
    `New neighbours arrive in ${townName} by invitation. Somebody who already lives here ` +
    "invites you, and that invitation is them vouching for you — which is why you arrive as a " +
    "member rather than as a stranger waiting to be recognised.",
  ask: (townName: string) =>
    `If you know somebody in ${townName}, ask them to send you one. It takes them a moment, ` +
    "and the link they send is all you need.",
  signIn: "Already have an account? Sign in",
} as const;

/**
 * What somebody following a link that no longer works is told.
 *
 * Deliberately vague about which of the four ways it stopped working, because
 * the server is: a used, withdrawn, lapsed and never-existed token are one
 * indistinguishable 404, so that nobody can use this page to discover which
 * addresses a town has invited. "Ask your neighbour for a fresh one" is true of
 * all four.
 */
export const INVITE_EXPIRED_NOTICE =
  "This invitation has expired or was already used. Ask your neighbour for a fresh one.";

/** The greeting over the registration form, naming who is vouching for them. */
export function invitedGreeting(inviterName: string, townName: string): string {
  const inviter = inviterName?.trim() || "A neighbour";
  const town = townName?.trim() || "the town";
  return `${inviter} invited you to join ${town} — welcome.`;
}

/**
 * Why the email field on an invited registration cannot be edited.
 *
 * The lock is not the rule; the server is, and it refuses a registration whose
 * address does not match the invitation's. Saying so is what keeps the disabled
 * field from reading as a broken form.
 */
export function invitedEmailNote(email: string): string {
  return `Your invitation is for ${email}, so that is the address you'll join with.`;
}

/** How the Neighbours page offers the action, and the login page's way in. */
export const INVITE_CTA = "Invite a neighbour";
export const LOGIN_INVITE_ONLY = "New here? The Bell is invitation-only";
export const LOGIN_INVITE_LINK = "See how to join";
