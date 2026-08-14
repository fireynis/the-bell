import { describe, expect, it } from "vitest";
import type { ApiError, Post, User } from "../api/types";
import {
  HOURLY_REPORT_LIMIT,
  MAX_REPORT_REASON_LENGTH,
  canReportPost,
  reportErrorMessage,
  validateReportReason,
} from "./report";

function post(overrides: Partial<Post> = {}): Post {
  return {
    id: "p1",
    author_id: "author",
    body: "body",
    image_path: "",
    status: "visible",
    created_at: "2026-03-01T12:00:00Z",
    edited_at: null,
    ...overrides,
  };
}

type Viewer = Pick<User, "id" | "role" | "is_active">;

function viewer(overrides: Partial<Viewer> = {}): Viewer {
  return {
    id: "reader",
    role: "member",
    is_active: true,
    ...overrides,
  };
}

function apiError(status: number, error = "boom"): ApiError {
  return { status, error };
}

describe("canReportPost", () => {
  it("lets a member report someone else's visible post", () => {
    expect(canReportPost(post(), viewer())).toBe(true);
  });

  it("refuses an anonymous reader, who has no session to report with", () => {
    expect(canReportPost(post(), null)).toBe(false);
  });

  // The card renders before the viewer's identity has loaded; unknown is not
  // permission.
  it("refuses while the viewer is still undefined", () => {
    expect(canReportPost(post(), undefined)).toBe(false);
  });

  it("refuses the author their own post, which the service rejects with a 400", () => {
    expect(canReportPost(post({ author_id: "reader" }), viewer())).toBe(false);
  });

  it("refuses a post that is no longer visible", () => {
    expect(canReportPost(post({ status: "removed_by_mod" }), viewer())).toBe(false);
  });

  it("refuses a suspended account, which the route guard answers with a 403", () => {
    expect(canReportPost(post(), viewer({ is_active: false }))).toBe(false);
  });

  // roleRank in internal/middleware/auth.go puts both below member.
  it("refuses a pending account", () => {
    expect(canReportPost(post(), viewer({ role: "pending" }))).toBe(false);
  });

  it("refuses a banned account", () => {
    expect(canReportPost(post(), viewer({ role: "banned" }))).toBe(false);
  });

  it("allows a moderator, who ranks above member", () => {
    expect(canReportPost(post(), viewer({ role: "moderator" }))).toBe(true);
  });

  it("allows a council member", () => {
    expect(canReportPost(post(), viewer({ role: "council" }))).toBe(true);
  });

  it("refuses a role the server has not taught this build about", () => {
    expect(canReportPost(post(), viewer({ role: "observer" }))).toBe(false);
  });
});

describe("validateReportReason", () => {
  it("accepts an ordinary reason", () => {
    expect(validateReportReason("This is someone's home address.")).toEqual({ valid: true });
  });

  it("rejects an empty reason, which moderators cannot triage", () => {
    expect(validateReportReason("").valid).toBe(false);
  });

  it("rejects a whitespace-only reason, as the server trims before checking", () => {
    expect(validateReportReason("  \n\t ").valid).toBe(false);
  });

  it("accepts a reason of exactly the maximum length", () => {
    expect(validateReportReason("a".repeat(MAX_REPORT_REASON_LENGTH)).valid).toBe(true);
  });

  it("rejects one byte over the maximum", () => {
    const result = validateReportReason("a".repeat(MAX_REPORT_REASON_LENGTH + 1));
    expect(result.valid).toBe(false);
    expect(result.error).toContain(String(MAX_REPORT_REASON_LENGTH));
  });

  // The server bounds len(reason) on a Go string, so the limit is in UTF-8
  // bytes. Measuring JS characters would accept this and be refused with a 400.
  it("counts a multi-byte character as its UTF-8 bytes, not as one character", () => {
    const emoji = "🔔".repeat(MAX_REPORT_REASON_LENGTH / 4);
    expect(emoji.length).toBeLessThan(MAX_REPORT_REASON_LENGTH);
    expect(validateReportReason(`${emoji}x`).valid).toBe(false);
  });

  it("measures after trimming, so surrounding whitespace does not blow the limit", () => {
    expect(validateReportReason(`  ${"a".repeat(MAX_REPORT_REASON_LENGTH)}  `).valid).toBe(true);
  });
});

describe("reportErrorMessage", () => {
  it("names the hourly limit on a 429, from either the middleware or the service", () => {
    expect(reportErrorMessage(apiError(429, "rate limit exceeded"))).toContain(
      String(HOURLY_REPORT_LIMIT),
    );
  });

  it("passes on the server's own complaint about a duplicate report", () => {
    const message = reportErrorMessage(
      apiError(400, "validation error: you have already reported this post"),
    );
    expect(message).toBe("You have already reported this post.");
  });

  it("strips the prefix and punctuates whatever else a 400 carries", () => {
    expect(reportErrorMessage(apiError(400, "validation error: cannot report your own post"))).toBe(
      "Cannot report your own post.",
    );
  });

  it("falls back to its own wording when a 400 arrives with nothing to say", () => {
    expect(reportErrorMessage(apiError(400, "validation error:"))).toContain("could not be sent");
  });

  it("explains a 403 as an account problem rather than a post problem", () => {
    expect(reportErrorMessage(apiError(403, "forbidden"))).toContain("account");
  });

  it("reports a 404 as the post being gone", () => {
    expect(reportErrorMessage(apiError(404, "not found"))).toContain("no longer available");
  });

  it("never leaks a 500's internal message", () => {
    const message = reportErrorMessage(apiError(500, "internal error"));
    expect(message).not.toContain("internal error");
  });

  it("has something to say when the failure carried no error at all", () => {
    expect(reportErrorMessage(null)).toContain("could not be sent");
  });
});
