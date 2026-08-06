import { useState } from "react";
import { moderationApi } from "../api/client.ts";
import type { ApiError } from "../api/types.ts";
import ErrorBanner from "./ErrorBanner.tsx";
import { MAX_REMOVAL_REASON_LENGTH, validateRemovalReason } from "../lib/moderation.ts";

const inputStyle: React.CSSProperties = {
  borderColor: "var(--color-border)",
  borderWidth: "1px",
  borderStyle: "solid",
  borderRadius: "var(--radius-md)",
  padding: "0.5rem 0.75rem",
  width: "100%",
  display: "block",
  outline: "none",
};

interface RemovePostDialogProps {
  postId: string;
  onClose: () => void;
  onRemoved: () => void;
}

/**
 * Collects the mandatory reason for taking a post down.
 *
 * The reason is stored as a moderator's private note and is never returned by
 * the API, so this dialog is the only place it is ever visible — there is no
 * reading it back later through the post.
 */
export default function RemovePostDialog({ postId, onClose, onRemoved }: RemovePostDialogProps) {
  const [reason, setReason] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [focused, setFocused] = useState(false);

  const check = validateRemovalReason(reason);
  const canSubmit = check.valid && !submitting;

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (submitting) return;
    if (!check.valid) {
      setError(check.error ?? null);
      return;
    }

    setSubmitting(true);
    setError(null);

    try {
      await moderationApi.removePost(postId, reason.trim());
      onRemoved();
    } catch (err) {
      const apiErr = err as ApiError;
      setError(apiErr.error ?? "Failed to remove the post.");
      setSubmitting(false);
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50">
      <div
        className="w-full max-w-md p-6"
        style={{
          backgroundColor: "var(--color-surface)",
          boxShadow: "var(--shadow-lg)",
          borderRadius: "var(--radius-lg)",
        }}
      >
        <h2 className="mb-2 text-lg font-semibold" style={{ color: "var(--color-text)" }}>
          Remove Post
        </h2>
        <p className="mb-4 text-sm" style={{ color: "var(--color-text-secondary)" }}>
          The post stops being visible to the town. Its author and moderators can still see it.
          This does not act against the author — use Take Action for that.
        </p>

        <form onSubmit={handleSubmit} className="flex flex-col gap-4">
          <div>
            <label
              htmlFor="removal-reason"
              className="mb-1 block text-sm font-medium"
              style={{ color: "var(--color-text-secondary)" }}
            >
              Reason
            </label>
            <textarea
              id="removal-reason"
              value={reason}
              onChange={(e) => setReason(e.target.value)}
              maxLength={MAX_REMOVAL_REASON_LENGTH}
              rows={3}
              style={{
                ...inputStyle,
                resize: "none",
                ...(focused
                  ? {
                      borderColor: "var(--color-primary)",
                      boxShadow: "0 0 0 1px var(--color-primary)",
                    }
                  : {}),
              }}
              onFocus={() => setFocused(true)}
              onBlur={() => setFocused(false)}
              placeholder="Describe why this post is being removed..."
            />
            <p className="mt-1 text-xs" style={{ color: "var(--color-text-tertiary)" }}>
              {reason.length}/{MAX_REMOVAL_REASON_LENGTH}
            </p>
          </div>

          {error && <ErrorBanner message={error} />}

          <div className="flex justify-end gap-3">
            <button
              type="button"
              onClick={onClose}
              className="rounded-md px-4 py-2 text-sm font-medium"
              style={{
                backgroundColor: "var(--color-surface-tertiary)",
                color: "var(--color-text-secondary)",
              }}
              onMouseEnter={(e) => {
                (e.currentTarget as HTMLButtonElement).style.filter = "brightness(0.95)";
              }}
              onMouseLeave={(e) => {
                (e.currentTarget as HTMLButtonElement).style.filter = "";
              }}
            >
              Cancel
            </button>
            <button
              type="submit"
              disabled={!canSubmit}
              className="rounded-md px-4 py-2 text-sm font-medium disabled:opacity-50"
              style={{
                backgroundColor: "var(--color-danger)",
                color: "var(--color-text-inverse)",
              }}
              onMouseEnter={(e) => {
                if (canSubmit) (e.currentTarget as HTMLButtonElement).style.filter = "brightness(1.1)";
              }}
              onMouseLeave={(e) => {
                (e.currentTarget as HTMLButtonElement).style.filter = "";
              }}
            >
              {submitting ? "Removing..." : "Remove Post"}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}
