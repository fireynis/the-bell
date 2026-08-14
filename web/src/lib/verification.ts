import type { ApiError } from "../api/types";

/**
 * The message middleware.RequireVerifiedEmail writes, verbatim.
 *
 * Matching on wording rather than on the 403 alone is deliberate, and the Go
 * comment beside it says why the wording is stable: "forbidden" and "account
 * suspended" tell a resident to go and ask a moderator, this one tells them to
 * open their inbox, and the client has to be able to tell those apart without
 * knowing which routes the guard is installed on.
 */
const UNVERIFIED_ERROR = "email not verified";

/** The route in routes.tsx that carries Kratos's verification flow. */
export const VERIFICATION_PATH = "/auth/verification";

/**
 * What an unverified member is told, wherever they meet the guard.
 *
 * One sentence for the banner and for each refused action, because they are the
 * same fact: the member did not do anything wrong, and there is exactly one
 * thing to do about it. It names the inbox rather than the rule, since "email
 * not verified" is a state and the message has to be an instruction.
 */
export const UNVERIFIED_EMAIL_NOTICE =
  "Your email address isn't verified yet — check your inbox for the verification message.";

/**
 * isEmailUnverified recognizes the one 403 that means "open your inbox".
 *
 * This is the whole reason a member could not previously tell that guard from
 * any other failure: GET /v1/me skips it — that route exists so somebody who
 * may not participate can still learn why — so the app renders signed in, and
 * then every other call is refused with a message nothing on this side read.
 *
 * Takes unknown because catch clauses do: the api client throws an ApiError
 * shaped object, but a network failure throws a TypeError, and neither the
 * caller nor this needs a cast to tell them apart.
 */
export function isEmailUnverified(err: unknown): boolean {
  const candidate = err as Partial<ApiError> | null | undefined;
  if (!candidate || candidate.status !== 403) return false;
  return typeof candidate.error === "string" && candidate.error.trim() === UNVERIFIED_ERROR;
}

/**
 * apiErrorMessage picks the sentence to show for a failed request: the
 * verification notice when that is what happened, then the server's own
 * wording, then the caller's fallback.
 *
 * The order matters. The server's message is usually the most accurate thing
 * anyone can say, which is why it comes before the fallback — but "email not
 * verified" is the exception, a three-word state that reads as a bug in the app
 * rather than something the member can act on.
 */
export function apiErrorMessage(err: unknown, fallback: string): string {
  if (isEmailUnverified(err)) return UNVERIFIED_EMAIL_NOTICE;

  const message = (err as Partial<ApiError> | null | undefined)?.error;
  return typeof message === "string" && message.trim().length > 0 ? message : fallback;
}
