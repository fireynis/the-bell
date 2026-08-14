import type { ApiError, Post, User } from "../api/types";
import { byteLength, validationDetail, type ValidationResult } from "./post";

/** Mirrors maxReportReasonLen in internal/service/report.go. */
export const MAX_REPORT_REASON_LENGTH = 1000;

/** Mirrors hourlyReportLimit in internal/service/report.go. */
export const HOURLY_REPORT_LIMIT = 5;

/**
 * The roles that rank at or above member in roleRank
 * (internal/middleware/auth.go), which is what the route guard on
 * POST /v1/posts/{id}/report requires. Pending and banned rank below it.
 */
const REPORTING_ROLES: readonly string[] = ["member", "moderator", "council"];

/** The subset of the viewer these checks read. */
type Reporter = Pick<User, "id" | "role" | "is_active"> | null | undefined;

/**
 * canReportPost reports whether to offer the control at all.
 *
 * Reporting is the one moderation affordance an ordinary member has, and it is
 * offered quietly: a control that appears and then refuses is worse than no
 * control, so every gate the request would hit is checked first. In order,
 * mirroring what would reject it:
 *
 *   - signed in, active, and member or above — the route guard in
 *     internal/server/routes.go, which answers 401/403;
 *   - the post is still visible, and is not the viewer's own — both checked by
 *     ReportService.SubmitReport, which answers 400.
 *
 * The server remains the authority; this only decides whether showing the item
 * is honest. Unlike the trust gates in gating.ts there is no explanatory
 * variant, because there is nothing a reporter can do to earn the ability to
 * report their own post.
 */
export function canReportPost(post: Pick<Post, "author_id" | "status">, viewer: Reporter): boolean {
  if (!viewer || !viewer.is_active) return false;
  if (!REPORTING_ROLES.includes(viewer.role)) return false;
  if (post.status !== "visible") return false;
  return post.author_id !== viewer.id;
}

/**
 * validateReportReason applies validateReportReason from
 * internal/service/report.go so the dialog can disable its submit button
 * instead of round-tripping to a guaranteed 400.
 *
 * Measured in UTF-8 bytes for the reason given on byteLength: the server bounds
 * `len(reason)` over a Go string, and the database column is bounded the same
 * way.
 */
export function validateReportReason(reason: string): ValidationResult {
  const trimmed = (reason ?? "").trim();
  if (trimmed.length === 0) {
    return { valid: false, error: "Please say what is wrong with this post." };
  }

  const bytes = byteLength(trimmed);
  if (bytes > MAX_REPORT_REASON_LENGTH) {
    return {
      valid: false,
      error: `Reason is ${bytes} bytes; the maximum is ${MAX_REPORT_REASON_LENGTH}.`,
    };
  }

  return { valid: true };
}

/**
 * reportErrorMessage turns a failed report into a sentence a neighbour can read.
 *
 * A 429 arrives from two places that mean the same thing — the "reports"
 * rate-limit middleware and ErrRateLimit raised by the service's own hourly
 * count — so both get the one message that names the actual limit.
 *
 * The 400s are the interesting ones: already reported, reporting your own post,
 * and reporting something no longer visible all land there, and the server's own
 * sentence distinguishes them better than any guess made from the status alone.
 */
export function reportErrorMessage(err: ApiError | null | undefined): string {
  const fallback = "Your report could not be sent. Please try again.";
  if (!err) return fallback;

  switch (err.status) {
    case 429:
      return `You can send ${HOURLY_REPORT_LIMIT} reports an hour, and you have used them all. Please try again later.`;
    case 404:
      return "That post is no longer available.";
    case 401:
    case 403:
      return "Your account cannot send reports right now.";
    case 400:
      return validationDetail(err.error) ?? fallback;
    default:
      return fallback;
  }
}
