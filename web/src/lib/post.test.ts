import { describe, expect, it } from "vitest";
import type { Post } from "../api/types";
import {
  EDIT_WINDOW_MINUTES,
  MAX_ALT_TEXT_LENGTH,
  MAX_IMAGE_BYTES,
  MAX_POST_BODY_LENGTH,
  appendFeedPage,
  byteLength,
  canDeletePost,
  canEditPost,
  counterColor,
  counterOpacity,
  postMutationErrorMessage,
  remainingAltTextChars,
  remainingChars,
  replacePost,
  runeLength,
  validateAltText,
  validateImageFile,
  validatePostBody,
  validationDetail,
  withoutPost,
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

describe("validateAltText", () => {
  it("accepts a description of an image", () => {
    expect(validateAltText("A heron on the frozen millpond", true)).toEqual({ valid: true });
  });

  // Describing your image is encouraged, never required — a validator that
  // treated an empty field as an error would make it required by the back door.
  it("accepts an empty description whether or not there is an image", () => {
    expect(validateAltText("", true)).toEqual({ valid: true });
    expect(validateAltText("", false)).toEqual({ valid: true });
    expect(validateAltText("   \n ", false)).toEqual({ valid: true });
  });

  it("rejects a description with no image to describe", () => {
    expect(validateAltText("A heron on the frozen millpond", false).valid).toBe(false);
  });

  it("accepts a description of exactly the maximum length", () => {
    expect(validateAltText("a".repeat(MAX_ALT_TEXT_LENGTH), true).valid).toBe(true);
  });

  it("rejects one character over the maximum", () => {
    expect(validateAltText("a".repeat(MAX_ALT_TEXT_LENGTH + 1), true).valid).toBe(false);
  });

  // The server counts runes here, unlike every other bound on the platform. A
  // byte count would reject this and disagree with the server about a
  // description the server would happily store.
  it("counts characters rather than bytes", () => {
    const emoji = "\u{1F514}".repeat(MAX_ALT_TEXT_LENGTH);
    expect(validateAltText(emoji, true).valid).toBe(true);
    expect(validateAltText(emoji + "\u{1F514}", true).valid).toBe(false);
  });

  // The server trims before bounding, so a description that only exceeds the
  // limit with its trailing whitespace is one the server accepts.
  it("measures the trimmed description", () => {
    expect(validateAltText("  " + "a".repeat(MAX_ALT_TEXT_LENGTH) + "  ", true).valid).toBe(true);
  });
});

describe("remainingAltTextChars", () => {
  it("counts down from the maximum", () => {
    expect(remainingAltTextChars("")).toBe(MAX_ALT_TEXT_LENGTH);
    expect(remainingAltTextChars("abc")).toBe(MAX_ALT_TEXT_LENGTH - 3);
  });

  it("counts an astral character once, the way the server does", () => {
    expect(remainingAltTextChars("\u{1F514}")).toBe(MAX_ALT_TEXT_LENGTH - 1);
  });

  it("goes negative past the limit", () => {
    expect(remainingAltTextChars("a".repeat(MAX_ALT_TEXT_LENGTH + 5))).toBe(-5);
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

describe("canDeletePost", () => {
  it("allows the author, with no window to be inside", () => {
    expect(canDeletePost(post({ created_at: minutesAgo(60 * 24) }), "author")).toBe(true);
  });

  it("refuses a different user", () => {
    expect(canDeletePost(post(), "someone-else")).toBe(false);
  });

  it("refuses when nobody is signed in", () => {
    expect(canDeletePost(post(), null)).toBe(false);
  });

  // The server would allow this — PostService.Delete checks only authorship —
  // but there is nothing left to take down, so the control is not offered.
  it.each(["removed_by_author", "removed_by_mod"])("refuses a %s post", (status) => {
    expect(canDeletePost(post({ status }), "author")).toBe(false);
  });
});

describe("byteLength", () => {
  it("counts plain ASCII as one byte each", () => {
    expect(byteLength("hello")).toBe(5);
  });

  it("counts an emoji as its four UTF-8 bytes, not its two UTF-16 units", () => {
    expect("🔔".length).toBe(2);
    expect(byteLength("🔔")).toBe(4);
  });

  it("counts an accented character as two bytes", () => {
    expect(byteLength("é")).toBe(2);
  });

  it("is zero for an empty string", () => {
    expect(byteLength("")).toBe(0);
  });
});

describe("runeLength", () => {
  it("counts plain ASCII as one character each", () => {
    expect(runeLength("hello")).toBe(5);
  });

  // The counterpart of the byteLength case above: a bound written in characters
  // on the server must not be measured in UTF-16 units, which would refuse a
  // field the server would have accepted.
  it("counts an emoji as one character, not as its two UTF-16 units", () => {
    expect("🔔".length).toBe(2);
    expect(runeLength("🔔")).toBe(1);
  });

  it("counts an accented character as one, whatever it costs in bytes", () => {
    expect(runeLength("é")).toBe(1);
    expect(byteLength("é")).toBe(2);
  });

  // A rune is a code point, exactly as in Go — a flag is two of them, and this
  // deliberately agrees with the server rather than with a human's idea of one
  // character.
  it("counts a flag as the code points it is made of", () => {
    expect(runeLength("🇨🇦")).toBe(2);
  });

  it("is zero for an empty string", () => {
    expect(runeLength("")).toBe(0);
  });
});

describe("validationDetail", () => {
  it("drops the sentinel prefix the API wraps every 400 in", () => {
    expect(validationDetail("validation error: reason must not be empty")).toBe(
      "Reason must not be empty.",
    );
  });

  it("punctuates a detail that came without a full stop", () => {
    expect(validationDetail("validation error: post is not visible")).toBe("Post is not visible.");
  });

  it("leaves punctuation the server already supplied alone", () => {
    expect(validationDetail("validation error: post is not visible.")).toBe("Post is not visible.");
  });

  it("uses a message that carries no prefix as-is", () => {
    expect(validationDetail("edit window expired")).toBe("Edit window expired.");
  });

  it("returns null for a prefix with nothing behind it", () => {
    expect(validationDetail("validation error:")).toBeNull();
  });

  it("returns null rather than a bare full stop for an absent message", () => {
    expect(validationDetail(undefined)).toBeNull();
    expect(validationDetail(null)).toBeNull();
  });
});

describe("postMutationErrorMessage", () => {
  // "edit window expired" is all the server says; the author needs to know how
  // long the window was and that nothing else is wrong with their post.
  it("explains a 409 as the edit window having closed, and how long it was", () => {
    const message = postMutationErrorMessage({ status: 409, error: "edit window expired" }, "edit");
    expect(message).toContain(String(EDIT_WINDOW_MINUTES));
  });

  it("explains a 429, which PATCH and DELETE share with posting", () => {
    expect(
      postMutationErrorMessage({ status: 429, error: "rate limit exceeded" }, "delete"),
    ).toContain("try again later");
  });

  it("names the action in a 403, so the author knows what was refused", () => {
    expect(postMutationErrorMessage({ status: 403, error: "forbidden" }, "delete")).toContain(
      "delete",
    );
  });

  it("passes on the server's own complaint for a 400", () => {
    expect(
      postMutationErrorMessage({ status: 400, error: "validation error: post cannot be empty" }, "edit"),
    ).toBe("Post cannot be empty.");
  });

  it("never leaks a 500's internal message", () => {
    const message = postMutationErrorMessage({ status: 500, error: "internal error" }, "edit");
    expect(message).not.toContain("internal error");
  });

  it("distinguishes the two actions when it has nothing else to go on", () => {
    expect(postMutationErrorMessage(null, "edit")).toContain("edit could not be saved");
    expect(postMutationErrorMessage(null, "delete")).toContain("could not be deleted");
  });
});

describe("replacePost", () => {
  it("swaps in the updated post", () => {
    const got = replacePost([post({ id: "a" }), post({ id: "b" })], post({ id: "b", body: "new" }));
    expect(got.map((p) => p.body)).toEqual(["body", "new"]);
  });

  // An edit is not a new post: a card that jumped to the top would read to
  // every other reader as an arrival.
  it("leaves the post where it was in the list", () => {
    const got = replacePost(
      [post({ id: "a" }), post({ id: "b" }), post({ id: "c" })],
      post({ id: "b", body: "new" }),
    );
    expect(got.map((p) => p.id)).toEqual(["a", "b", "c"]);
  });

  it("changes nothing when the post is not in the list", () => {
    const existing = [post({ id: "a" })];
    expect(replacePost(existing, post({ id: "z", body: "new" }))).toEqual(existing);
  });

  it("does not mutate the array it was given", () => {
    const existing = [post({ id: "a", body: "old" })];
    replacePost(existing, post({ id: "a", body: "new" }));
    expect(existing[0].body).toBe("old");
  });
});

describe("withoutPost", () => {
  it("drops the deleted post", () => {
    const got = withoutPost([post({ id: "a" }), post({ id: "b" })], "a");
    expect(got.map((p) => p.id)).toEqual(["b"]);
  });

  it("changes nothing when the post is not in the list", () => {
    expect(withoutPost([post({ id: "a" })], "z").map((p) => p.id)).toEqual(["a"]);
  });

  it("does not mutate the array it was given", () => {
    const existing = [post({ id: "a" })];
    withoutPost(existing, "a");
    expect(existing).toHaveLength(1);
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

// The counter is expressed in characters remaining, but what a reader thinks
// in is characters typed, so the thresholds are stated both ways: bodyOf(n)
// builds a post of n characters and remainingChars converts it back.
function remainingFor(length: number): number {
  return remainingChars("a".repeat(length));
}

describe("counterColor", () => {
  it("is green for a short post", () => {
    expect(counterColor(remainingFor(0))).toBe("#16a34a");
  });

  it("is still green just before the amber threshold at 950 characters", () => {
    expect(counterColor(remainingFor(949))).toBe("#16a34a");
  });

  it("turns amber at 950 characters", () => {
    expect(counterColor(remainingFor(950))).toBe("#ca8a04");
  });

  it("is still amber just before the red threshold at 980 characters", () => {
    expect(counterColor(remainingFor(979))).toBe("#ca8a04");
  });

  it("turns red at 980 characters", () => {
    expect(counterColor(remainingFor(980))).toBe("var(--color-danger)");
  });

  it("stays red at the limit", () => {
    expect(counterColor(remainingFor(MAX_POST_BODY_LENGTH))).toBe("var(--color-danger)");
  });

  // Over-long bodies are possible: the textarea is not capped, validation is.
  it("stays red past the limit, where remaining is negative", () => {
    expect(counterColor(remainingFor(MAX_POST_BODY_LENGTH + 50))).toBe("var(--color-danger)");
  });
});

describe("counterOpacity", () => {
  it("is fully hidden for a short post", () => {
    expect(counterOpacity(remainingFor(0))).toBe(0);
  });

  it("is still hidden one character before the fade begins at 900", () => {
    expect(counterOpacity(remainingFor(899))).toBe(0);
  });

  it("begins the fade at 900 characters", () => {
    expect(counterOpacity(remainingFor(900))).toBe(0);
  });

  // The ramp is (length - 900) / 20 across the 900-920 window.
  it("is half faded in at 910 characters", () => {
    expect(counterOpacity(remainingFor(910))).toBeCloseTo(0.5);
  });

  it("is three quarters faded in at 915 characters", () => {
    expect(counterOpacity(remainingFor(915))).toBeCloseTo(0.75);
  });

  it("is fully opaque at 920 characters", () => {
    expect(counterOpacity(remainingFor(920))).toBe(1);
  });

  it("stays fully opaque beyond 920", () => {
    expect(counterOpacity(remainingFor(999))).toBe(1);
  });

  it("stays fully opaque past the limit", () => {
    expect(counterOpacity(remainingFor(MAX_POST_BODY_LENGTH + 50))).toBe(1);
  });

  it("never leaves the 0 to 1 range across the whole ramp", () => {
    for (let length = 890; length <= 930; length++) {
      const opacity = counterOpacity(remainingFor(length));
      expect(opacity).toBeGreaterThanOrEqual(0);
      expect(opacity).toBeLessThanOrEqual(1);
    }
  });
});
