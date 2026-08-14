import { useState } from "react";
import { postApi } from "../api/client.ts";
import type { ApiError, Post } from "../api/types.ts";
import ErrorBanner from "./ErrorBanner.tsx";
import Spinner from "./Spinner.tsx";
import { useModalDialog } from "../hooks/useModalDialog.ts";
import {
  MAX_POST_BODY_LENGTH,
  counterColor,
  counterOpacity,
  postMutationErrorMessage,
  remainingChars,
  validatePostBody,
} from "../lib/post.ts";

interface EditPostDialogProps {
  post: Post;
  onClose: () => void;
  /** Handed the server's copy of the post, which carries the new edited_at. */
  onSaved: (post: Post) => void;
}

/**
 * Edits an author's own post in place.
 *
 * A dialog rather than an inline textarea on the card: the feed is a live list
 * that reorders under the reader as posts arrive, and a form that is part of a
 * card can be scrolled away or pushed down mid-sentence.
 *
 * Only the body is editable. PATCH /v1/posts/{id} takes nothing else — an image
 * cannot be swapped or removed after the fact — so the image is left visible on
 * the card behind rather than shown here as if it could be changed.
 */
export default function EditPostDialog({ post, onClose, onSaved }: EditPostDialogProps) {
  const [body, setBody] = useState(post.body);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const panelRef = useModalDialog<HTMLDivElement>(() => {
    if (!saving) onClose();
  });

  const check = validatePostBody(body);
  const unchanged = body === post.body;
  const canSave = check.valid && !saving && !unchanged;
  const remaining = remainingChars(body);

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (saving) return;
    if (!check.valid) {
      setError(check.error ?? null);
      return;
    }

    setSaving(true);
    setError(null);

    try {
      const updated = await postApi.update(post.id, body.trim());
      onSaved(updated);
      onClose();
    } catch (err) {
      setError(postMutationErrorMessage(err as ApiError, "edit"));
      setSaving(false);
    }
  }

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4"
      role="dialog"
      aria-modal="true"
      aria-label="Edit your post"
    >
      <div
        ref={panelRef}
        className="w-full max-w-md p-6"
        style={{
          backgroundColor: "var(--color-surface)",
          boxShadow: "var(--shadow-lg)",
          borderRadius: "var(--radius-lg)",
        }}
      >
        <h2 className="mb-2 text-lg font-semibold" style={{ color: "var(--color-text)" }}>
          Edit your post
        </h2>
        <p className="mb-4 text-sm leading-relaxed" style={{ color: "var(--color-text-secondary)" }}>
          Neighbours who have already read it will see the new wording, marked as edited.
        </p>

        <form onSubmit={handleSubmit} className="flex flex-col gap-4">
          <div>
            <label
              htmlFor="edit-post-body"
              className="mb-1 block text-sm font-medium"
              style={{ color: "var(--color-text-secondary)" }}
            >
              Post
            </label>
            <textarea
              id="edit-post-body"
              value={body}
              onChange={(e) => setBody(e.target.value)}
              rows={6}
              disabled={saving}
              className="w-full resize-none leading-relaxed focus:outline-none"
              style={{
                borderColor: "var(--color-border)",
                borderWidth: "1px",
                borderStyle: "solid",
                borderRadius: "var(--radius-md)",
                padding: "0.5rem 0.75rem",
                color: "var(--color-text)",
                backgroundColor: "var(--color-surface)",
                fontSize: "0.9375rem",
              }}
            />
            {/* The same progressive counter the composer uses, on the same limit. */}
            <p
              className="mt-1 text-xs"
              style={{
                color: counterColor(remaining),
                opacity: counterOpacity(remaining),
                transition: "color 0.3s, opacity 0.3s",
              }}
            >
              {body.length} / {MAX_POST_BODY_LENGTH}
            </p>
          </div>

          {error && <ErrorBanner message={error} />}

          <div className="flex justify-end gap-3">
            <button
              type="button"
              onClick={onClose}
              disabled={saving}
              className="rounded-md px-4 py-2 text-sm font-medium disabled:opacity-50"
              style={{
                backgroundColor: "var(--color-surface-tertiary)",
                color: "var(--color-text-secondary)",
              }}
            >
              Cancel
            </button>
            <button
              type="submit"
              disabled={!canSave}
              className="rounded-md px-4 py-2 text-sm font-medium disabled:cursor-not-allowed disabled:opacity-50"
              style={{
                backgroundColor: "var(--color-primary)",
                color: "var(--color-text-inverse)",
              }}
            >
              {saving ? (
                <span className="inline-flex items-center gap-2">
                  <Spinner size="sm" />
                  Saving...
                </span>
              ) : (
                "Save changes"
              )}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}
