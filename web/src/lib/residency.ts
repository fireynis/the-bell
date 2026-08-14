import type { ApiError, User } from "../api/types";
import { runeLength, validationDetail, type ValidationResult } from "./post";
import { UNVERIFIED_EMAIL_NOTICE, isEmailUnverified } from "./verification";

/**
 * Where in town somebody says they are.
 *
 * The claim is an attestation and never a fact. Nothing checks it, nothing
 * geocodes it, and the town does not act on it — it exists so that the council
 * member deciding whether to ring a stranger in has something to recognise them
 * by beyond a name and a join date. Everything in this module is written to keep
 * that reading: the wording the queue shows says who is claiming it, and the
 * prompt asks rather than requires.
 *
 * It is also the most sensitive thing a member tells this app, which is why it
 * appears on the approval queue and the member's own form and absolutely nowhere
 * else — not on a profile, not in the directory, not beside a post. The server
 * draws the same line; this side draws it by simply having no helper that
 * renders a claim for a general audience.
 */

/**
 * Mirrors the server's bound on the claim: 0..300 characters after trimming,
 * where the empty string is a member clearing it rather than a validation
 * failure.
 *
 * Characters, not bytes — measured with runeLength for the reason given there.
 */
export const MAX_RESIDENCY_CLAIM_LENGTH = 300;

/**
 * residencyClaimOf reads the claim off a profile, for a build that may be
 * running against a server which does not send it on the self view.
 *
 * Returns the empty string for absent, null and whitespace-only alike, so a
 * caller can prefill an input without distinguishing "never set" from "not sent
 * on this response" — neither is something a text box can show.
 */
export function residencyClaimOf(user: Pick<User, "residency_claim"> | null | undefined): string {
  return (user?.residency_claim ?? "").trim();
}

/**
 * validateResidencyClaim applies the server's rule so the form can say what is
 * wrong instead of round-tripping to a 400.
 *
 * An empty claim is valid: clearing what you said is a legitimate thing to want
 * to do, and the endpoint documents the empty string as the way to do it. Only
 * length can fail.
 */
export function validateResidencyClaim(claim: string): ValidationResult {
  const chars = runeLength((claim ?? "").trim());
  if (chars > MAX_RESIDENCY_CLAIM_LENGTH) {
    return {
      valid: false,
      error: `That is ${chars} characters; the most you can say here is ${MAX_RESIDENCY_CLAIM_LENGTH}.`,
    };
  }
  return { valid: true };
}

/**
 * describeResidencyClaim renders a claim for the council's approval queue —
 * as something a person said, never as something the town knows.
 *
 * The subject of the sentence is the pending member, not the app: "Says they're
 * at or near Mill Lane" cannot be misread as an address the system verified,
 * whereas printing the bare string under somebody's name can. A council member
 * approving a stranger is making a judgement call, and the wording is what keeps
 * them making it about a claim rather than about a record.
 *
 * Returns null when there is nothing to show, so the caller renders its own
 * quiet line rather than an attribution attached to nothing.
 */
export function describeResidencyClaim(claim: string | null | undefined): string | null {
  const said = (claim ?? "").trim();
  if (!said) return null;
  return `Says they're at or near ${said}`;
}

/** What the queue shows for somebody who gave no claim. Not a reproach — most won't. */
export const NO_RESIDENCY_CLAIM = "No address given";

/**
 * residencyClaimErrorMessage turns a failed save into a sentence.
 *
 * The 400 carries the server's own wording, which distinguishes "too long" from
 * anything it may come to reject later better than a guess from the status can.
 */
export function residencyClaimErrorMessage(err: ApiError | null | undefined): string {
  const fallback = "That could not be saved just now. Please try again.";
  if (!err) return fallback;

  // Ahead of the switch, as everywhere else: the verification guard answers 403
  // too, and it has nothing to do with what was typed.
  if (isEmailUnverified(err)) return UNVERIFIED_EMAIL_NOTICE;

  switch (err.status) {
    case 400:
      return validationDetail(err.error) ?? fallback;
    case 401:
    case 403:
      return "Your account cannot save that right now.";
    default:
      return fallback;
  }
}
