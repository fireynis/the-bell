import type { ApiError, Post } from "../api/types";

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
 * byteLength measures a string the way Go's `len` does.
 *
 * Every server-side length bound on free text is `len(s)` over a Go string,
 * which counts UTF-8 bytes. Checking JS string length instead would wave
 * through a reason of multi-byte characters that the server then rejects, so
 * anything mirroring one of those bounds measures with this.
 */
export function byteLength(value: string): number {
  return new TextEncoder().encode(value).length;
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
 * canDeletePost reports whether to offer an author the control that takes their
 * own post down.
 *
 * PostService.Delete in internal/service/post.go checks only authorship — there
 * is no window, which is the whole difference from canEditPost. The visibility
 * check is this UI's own rule rather than the server's: deleting a post that a
 * moderator has already removed would succeed and change nothing a reader can
 * see, so offering it only invites the author to act on a post that is already
 * gone.
 */
export function canDeletePost(
  post: Pick<Post, "author_id" | "status">,
  userId: string | null,
): boolean {
  return !!userId && post.author_id === userId && post.status === "visible";
}

/**
 * validationDetail pulls the readable half out of a 400 from the API.
 *
 * statusForError in internal/handler/respond.go answers ErrValidation with the
 * whole error string, and those are built as `fmt.Errorf("%w: ...",
 * ErrValidation)` — so the body reads "validation error: you have already
 * reported this post" and only the part after the prefix is worth showing.
 * Returns null when there is nothing usable, leaving the caller's own wording to
 * stand.
 */
export function validationDetail(message: string | undefined | null): string | null {
  const detail = (message ?? "").replace(/^validation error:\s*/i, "").trim();
  if (detail.length === 0) return null;

  const sentence = detail[0].toUpperCase() + detail.slice(1);
  return /[.!?]$/.test(sentence) ? sentence : `${sentence}.`;
}

/**
 * postMutationErrorMessage turns a failed edit or delete into something the
 * author can act on.
 *
 * The 409 is the one worth naming outright: ErrEditWindow is answered with the
 * bare string "edit window expired", which says nothing about how long the
 * window was or that the post is otherwise fine. The 429 is reachable for both
 * actions because PATCH and DELETE sit inside the same 10-per-hour "posts"
 * bucket as creating one — see the guard in internal/server/routes.go.
 */
export function postMutationErrorMessage(
  err: ApiError | null | undefined,
  action: "edit" | "delete",
): string {
  const fallback =
    action === "edit"
      ? "Your edit could not be saved. Please try again."
      : "The post could not be deleted. Please try again.";

  if (!err) return fallback;

  switch (err.status) {
    case 409:
      return `Posts can only be edited for ${EDIT_WINDOW_MINUTES} minutes after they go up, and that time has passed.`;
    case 429:
      return "You have made a lot of changes in the last hour. Please try again later.";
    case 404:
      return "That post is no longer available.";
    case 401:
    case 403:
      return `Only the author can ${action} this post.`;
    case 400:
      return validationDetail(err.error) ?? fallback;
    default:
      return fallback;
  }
}

/**
 * replacePost swaps in a post the server has just returned, leaving the rest of
 * the list — and crucially its order — alone.
 *
 * An edit must not move a post in the feed: the author changed a typo, they did
 * not post again, and a card that jumps to the top would read to everyone else
 * as a new arrival.
 */
export function replacePost(posts: readonly Post[], updated: Post): Post[] {
  return posts.map((p) => (p.id === updated.id ? updated : p));
}

/** withoutPost drops a post the author has deleted from a loaded list. */
export function withoutPost(posts: readonly Post[], postId: string): Post[] {
  return posts.filter((p) => p.id !== postId);
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
