import { useState } from "react";
import { postApi } from "../api/client.ts";
import type { ApiError } from "../api/types.ts";
import ErrorBanner from "./ErrorBanner.tsx";
import Spinner from "./Spinner.tsx";
import { useModalDialog } from "../hooks/useModalDialog.ts";
import { byteLength } from "../lib/post.ts";
import {
  MAX_REPORT_REASON_LENGTH,
  reportErrorMessage,
  validateReportReason,
} from "../lib/report.ts";

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

interface ReportDialogProps {
  postId: string;
  /** Whose post is being reported, for the dialog's own accessible name. */
  authorName: string;
  onClose: () => void;
  /** Called once the report is filed, so the card can stop offering to file it again. */
  onReported: () => void;
}

/**
 * Collects why a member is reporting someone else's post.
 *
 * The reason is what a moderator reads to triage the queue, so it is required
 * and the dialog says as much rather than accepting an empty one and letting the
 * server refuse it.
 *
 * On success the form is replaced by an acknowledgement instead of the dialog
 * vanishing. Reporting a neighbour is not a nothing act, and the report goes
 * into a queue rather than doing anything visible — without a word back, the
 * only evidence anything happened is a dialog that closed.
 */
export default function ReportDialog({
  postId,
  authorName,
  onClose,
  onReported,
}: ReportDialogProps) {
  const [reason, setReason] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [focused, setFocused] = useState(false);
  const [sent, setSent] = useState(false);

  const panelRef = useModalDialog<HTMLDivElement>(() => {
    if (!submitting) onClose();
  });

  const check = validateReportReason(reason);
  const canSubmit = check.valid && !submitting;
  const bytes = byteLength(reason.trim());

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
      await postApi.report(postId, reason.trim());
      setSent(true);
      onReported();
    } catch (err) {
      setError(reportErrorMessage(err as ApiError));
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4"
      role="dialog"
      aria-modal="true"
      aria-label={`Report ${authorName}'s post`}
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
        {sent ? (
          <>
            <h2 className="mb-2 text-lg font-semibold" style={{ color: "var(--color-text)" }}>
              Thanks &mdash; a moderator will take a look
            </h2>
            <p
              className="mb-5 text-sm leading-relaxed"
              style={{ color: "var(--color-text-secondary)" }}
              role="status"
            >
              The post stays up for now. Moderators read every report, and nothing you send is
              shown to the person who posted it.
            </p>
            <div className="flex justify-end">
              <button
                type="button"
                onClick={onClose}
                className="rounded-md px-4 py-2 text-sm font-medium"
                style={{
                  backgroundColor: "var(--color-primary)",
                  color: "var(--color-text-inverse)",
                }}
              >
                Done
              </button>
            </div>
          </>
        ) : (
          <>
            <h2 className="mb-2 text-lg font-semibold" style={{ color: "var(--color-text)" }}>
              Report this post
            </h2>
            <p className="mb-4 text-sm leading-relaxed" style={{ color: "var(--color-text-secondary)" }}>
              A moderator will read this and decide what to do. {authorName} is not told who
              reported them.
            </p>

            <form onSubmit={handleSubmit} className="flex flex-col gap-4">
              <div>
                <label
                  htmlFor="report-reason"
                  className="mb-1 block text-sm font-medium"
                  style={{ color: "var(--color-text-secondary)" }}
                >
                  What is wrong with it?
                </label>
                <textarea
                  id="report-reason"
                  value={reason}
                  onChange={(e) => setReason(e.target.value)}
                  rows={3}
                  disabled={submitting}
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
                  placeholder="Tell the moderators what they should look at..."
                />
                {/*
                  Counted in bytes, and only once the reason is long enough for
                  the limit to be in sight — the number is the server's rule, not
                  a target, and showing it against a two-word report would be
                  noise.
                */}
                {bytes > MAX_REPORT_REASON_LENGTH / 2 && (
                  <p className="mt-1 text-xs" style={{ color: "var(--color-text-tertiary)" }}>
                    {bytes}/{MAX_REPORT_REASON_LENGTH} bytes
                  </p>
                )}
              </div>

              {error && <ErrorBanner message={error} />}

              <div className="flex justify-end gap-3">
                <button
                  type="button"
                  onClick={onClose}
                  disabled={submitting}
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
                  disabled={!canSubmit}
                  className="rounded-md px-4 py-2 text-sm font-medium disabled:opacity-50"
                  style={{
                    backgroundColor: "var(--color-primary)",
                    color: "var(--color-text-inverse)",
                  }}
                >
                  {submitting ? (
                    <span className="inline-flex items-center gap-2">
                      <Spinner size="sm" />
                      Sending...
                    </span>
                  ) : (
                    "Send report"
                  )}
                </button>
              </div>
            </form>
          </>
        )}
      </div>
    </div>
  );
}
