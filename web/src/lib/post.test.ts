import { describe, expect, it } from "vitest";
import type { Post } from "../api/types";
import {
  EDIT_WINDOW_MINUTES,
  MAX_IMAGE_BYTES,
  MAX_POST_BODY_LENGTH,
  appendFeedPage,
  canEditPost,
  remainingChars,
  validateImageFile,
  validatePostBody,
} from "./post";

const NOW = Date.parse("2026-03-01T12:00:00Z");

function post(overrides: Partial<Post> = {}): Post {
  return {
    id: "p1",
    author_id: "author",
    body: "body",
    image_path: "",
    status: "visible",
    created_at: new Date(NOW).toISOString(),
    edited_at: null,
    ...overrides,
  };
}

/** minutesAgo returns an ISO timestamp the given number of minutes before NOW. */
function minutesAgo(minutes: number): string {
  return new Date(NOW - minutes * 60_000).toISOString();
}

describe("validatePostBody", () => {
  it("accepts ordinary text", () => {
    expect(validatePostBody("Town hall is at 7pm")).toEqual({ valid: true });
  });

  it("rejects an empty body", () => {
    expect(validatePostBody("").valid).toBe(false);
  });

  it("rejects whitespace-only text", () => {
    expect(validatePostBody("   \n\t  ").valid).toBe(false);
  });

  it("accepts a body of exactly the maximum length", () => {
    expect(validatePostBody("a".repeat(MAX_POST_BODY_LENGTH)).valid).toBe(true);
  });

  it("rejects one character over the maximum", () => {
    const result = validatePostBody("a".repeat(MAX_POST_BODY_LENGTH + 1));
    expect(result.valid).toBe(false);
    expect(result.error).toContain(String(MAX_POST_BODY_LENGTH));
  });

  // Length is measured on the raw body because that is what the server stores,
  // so padding a too-long post with spaces must not sneak it through.
  it("counts untrimmed length against the limit", () => {
    const body = "a".repeat(MAX_POST_BODY_LENGTH) + "    ";
    expect(validatePostBody(body).valid).toBe(false);
  });
});

describe("remainingChars", () => {
  it("reports the full budget for an empty body", () => {
    expect(remainingChars("")).toBe(MAX_POST_BODY_LENGTH);
  });

  it("goes negative once over the limit, so the counter can warn", () => {
    expect(remainingChars("a".repeat(MAX_POST_BODY_LENGTH + 5))).toBe(-5);
  });
});

describe("validateImageFile", () => {
  it.each(["image/jpeg", "image/png", "image/webp"])("accepts %s", (type) => {
    expect(validateImageFile({ type, size: 1024 })).toEqual({ valid: true });
  });

  it.each(["image/gif", "application/pdf", "text/html", ""])("rejects %s", (type) => {
    expect(validateImageFile({ type, size: 1024 }).valid).toBe(false);
  });

  it("accepts a file of exactly the maximum size", () => {
    expect(validateImageFile({ type: "image/png", size: MAX_IMAGE_BYTES }).valid).toBe(true);
  });

  it("rejects one byte over the maximum", () => {
    const result = validateImageFile({ type: "image/png", size: MAX_IMAGE_BYTES + 1 });
    expect(result.valid).toBe(false);
    expect(result.error).toContain("5 MB");
  });

  // Type is checked before size so an unsupported format is named as such
  // rather than being reported as too large.
  it("reports the type problem first for an oversized unsupported file", () => {
    const result = validateImageFile({ type: "video/mp4", size: MAX_IMAGE_BYTES * 2 });
    expect(result.error).toContain("JPEG");
  });
});

describe("canEditPost", () => {
  it("allows the author inside the window", () => {
    expect(canEditPost(post({ created_at: minutesAgo(5) }), "author", NOW)).toBe(true);
  });

  it("allows editing at exactly the window boundary", () => {
    expect(
      canEditPost(post({ created_at: minutesAgo(EDIT_WINDOW_MINUTES) }), "author", NOW),
    ).toBe(true);
  });

  it("refuses just past the window", () => {
    expect(
      canEditPost(post({ created_at: minutesAgo(EDIT_WINDOW_MINUTES + 0.1) }), "author", NOW),
    ).toBe(false);
  });

  it("refuses a different user", () => {
    expect(canEditPost(post(), "someone-else", NOW)).toBe(false);
  });

  it("refuses when nobody is signed in", () => {
    expect(canEditPost(post(), null, NOW)).toBe(false);
  });

  it.each(["removed_by_author", "removed_by_mod"])("refuses a %s post", (status) => {
    expect(canEditPost(post({ status }), "author", NOW)).toBe(false);
  });

  it("refuses an unparseable timestamp rather than allowing the edit", () => {
    expect(canEditPost(post({ created_at: "not a date" }), "author", NOW)).toBe(false);
  });
});

describe("appendFeedPage", () => {
  it("appends a page after the existing posts", () => {
    const got = appendFeedPage([post({ id: "a" })], [post({ id: "b" })]);
    expect(got.map((p) => p.id)).toEqual(["a", "b"]);
  });

  // Cursor pages can overlap when a post is created between requests; without
  // dedup the same post renders twice and React warns about duplicate keys.
  it("drops posts already loaded", () => {
    const got = appendFeedPage([post({ id: "a" })], [post({ id: "a" }), post({ id: "b" })]);
    expect(got.map((p) => p.id)).toEqual(["a", "b"]);
  });

  it("deduplicates within the incoming page itself", () => {
    const got = appendFeedPage([], [post({ id: "x" }), post({ id: "x" })]);
    expect(got.map((p) => p.id)).toEqual(["x"]);
  });

  it("returns the existing posts unchanged for an empty page", () => {
    const existing = [post({ id: "a" })];
    expect(appendFeedPage(existing, [])).toEqual(existing);
  });

  it("does not mutate the array it was given", () => {
    const existing = [post({ id: "a" })];
    appendFeedPage(existing, [post({ id: "b" })]);
    expect(existing).toHaveLength(1);
  });

  it("skips malformed entries", () => {
    const got = appendFeedPage([], [post({ id: "ok" }), null as unknown as Post]);
    expect(got.map((p) => p.id)).toEqual(["ok"]);
  });
});
