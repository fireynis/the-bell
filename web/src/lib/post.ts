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
