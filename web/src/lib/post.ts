import type { Post } from "../api/types";

/** Mirrors domain.MaxPostBodyLength in internal/domain/post.go. */
export const MAX_POST_BODY_LENGTH = 1000;

/** Mirrors domain.EditWindowMinutes in internal/domain/post.go. */
export const EDIT_WINDOW_MINUTES = 15;

/** Mirrors maxImageSize in internal/handler/upload.go (5 MB). */
export const MAX_IMAGE_BYTES = 5 * 1024 * 1024;

/** Mirrors allowedImageTypes in internal/handler/upload.go. */
export const ALLOWED_IMAGE_TYPES = ["image/jpeg", "image/png", "image/webp"] as const;

export interface ValidationResult {
  valid: boolean;
  /** Human-readable reason, present only when invalid. */
  error?: string;
}

/**
 * validatePostBody applies the same rules as the server so the compose box can
 * disable submission instead of round-tripping to a guaranteed 400.
 *
 * Length is measured on the raw body because the server bounds the stored
 * string, but emptiness is measured after trimming so whitespace alone does not
 * count as a post.
 */
export function validatePostBody(body: string): ValidationResult {
  if (body.trim().length === 0) {
    return { valid: false, error: "Post cannot be empty." };
  }
  if (body.length > MAX_POST_BODY_LENGTH) {
    return {
      valid: false,
      error: `Post is ${body.length} characters; the maximum is ${MAX_POST_BODY_LENGTH}.`,
    };
  }
  return { valid: true };
}

/** remainingChars reports how many characters are left, going negative when over. */
export function remainingChars(body: string): number {
  return MAX_POST_BODY_LENGTH - body.length;
}

/**
 * counterColor grades the character counter from calm to alarming as the limit
 * approaches: green until 50 characters remain, amber from there, red for the
 * last 20.
 *
 * Both boundaries are inclusive of the tighter colour, so the counter has
 * already turned red by the time it reaches the number it warns about rather
 * than one keystroke after.
 */
export function counterColor(remaining: number): string {
  if (remaining <= 20) return "var(--color-danger)";
  if (remaining <= 50) return "#ca8a04"; // yellow-600
  return "#16a34a"; // green-600
}

/**
 * counterOpacity fades the character counter in over the twenty characters
 * before 100 remain, so it is invisible for an ordinary short post and fully
 * present well before the limit matters.
 *
 * Returning a ramp rather than toggling visibility avoids the counter popping
 * into existence mid-sentence and pulling the eye off the text box. The ramp is
 * bounded by its own branch — `remaining` is in (80, 100] there, so the result
 * lands in [0, 1) without needing a clamp.
 */
export function counterOpacity(remaining: number): number {
  if (remaining > 100) return 0;
  if (remaining > 80) return (100 - remaining) / 20;
  return 1;
}

/**
 * validateImageFile checks type and size before upload. The server re-checks
 * the real magic bytes; this only avoids sending a file that cannot succeed.
 */
export function validateImageFile(file: Pick<File, "type" | "size">): ValidationResult {
  if (!ALLOWED_IMAGE_TYPES.includes(file.type as (typeof ALLOWED_IMAGE_TYPES)[number])) {
    return { valid: false, error: "Only JPEG, PNG, and WebP images are supported." };
  }
  if (file.size > MAX_IMAGE_BYTES) {
    return { valid: false, error: "Image is too large; the maximum size is 5 MB." };
  }
  return { valid: true };
}

/**
 * canEditPost mirrors Post.CanEdit in internal/domain/post.go: only the author
 * may edit, only while the post is visible, and only inside the edit window.
 */
export function canEditPost(
  post: Pick<Post, "author_id" | "status" | "created_at">,
  userId: string | null,
  now: number = Date.now(),
): boolean {
  if (!userId || post.author_id !== userId) return false;
  if (post.status !== "visible") return false;

  const created = new Date(post.created_at).getTime();
  if (Number.isNaN(created)) return false;

  const elapsedMinutes = (now - created) / 60_000;
  return elapsedMinutes >= 0 && elapsedMinutes <= EDIT_WINDOW_MINUTES;
}

/**
 * appendFeedPage adds a page of results to the posts already loaded, dropping
 * any duplicate IDs.
 *
 * A cursor page can overlap the previous one when a post is created between
 * requests; without this the same post would render twice and React would warn
 * about duplicate keys.
 */
export function appendFeedPage(existing: readonly Post[], page: readonly Post[]): Post[] {
  const seen = new Set(existing.map((p) => p.id));
  const merged = [...existing];

  for (const post of page) {
    if (!post || typeof post.id !== "string" || seen.has(post.id)) continue;
    seen.add(post.id);
    merged.push(post);
  }

  return merged;
}
