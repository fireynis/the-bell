import ErrorBanner from "./ErrorBanner";
import Spinner from "./Spinner";
import { useModalDialog } from "../hooks/useModalDialog";

interface ConfirmDialogProps {
  title: string;
  /** What the action will do, in full. Shown before the confirm button. */
  body: string;
  confirmLabel: string;
  cancelLabel?: string;
  /** Styles the confirm button as destructive. */
  danger?: boolean;
  busy?: boolean;
  /** Surfaced above the buttons so a failed action can be retried in place. */
  error?: string | null;
  onConfirm: () => void;
  onCancel: () => void;
}

/**
 * ConfirmDialog asks before an action that cannot simply be undone.
 *
 * The body is required rather than optional: a dialog that only asks "are you
 * sure?" moves the decision without informing it, which is how people end up
 * confirming something they would have declined had it been spelled out.
 */
export default function ConfirmDialog({
  title,
  body,
  confirmLabel,
  cancelLabel = "Cancel",
  danger = false,
  busy = false,
  error = null,
  onConfirm,
  onCancel,
}: ConfirmDialogProps) {
  // Cancel is the first control in the panel, so the safe half of the choice
  // takes focus on open, never the destructive one. Escape is ignored while the
  // action is in flight, matching the disabled Cancel button — the request is
  // already gone, and closing would only hide the outcome.
  const panelRef = useModalDialog<HTMLDivElement>(() => {
    if (!busy) onCancel();
  });

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4"
      role="dialog"
      aria-modal="true"
      aria-label={title}
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
        <h2 className="mb-3 text-lg font-semibold" style={{ color: "var(--color-text)" }}>
          {title}
        </h2>

        <p className="mb-4 text-sm leading-relaxed" style={{ color: "var(--color-text-secondary)" }}>
          {body}
        </p>

        {error && (
          <div className="mb-4">
            <ErrorBanner message={error} />
          </div>
        )}

        <div className="flex justify-end gap-3">
          <button
            type="button"
            onClick={onCancel}
            disabled={busy}
            className="rounded-md px-4 py-2 text-sm font-medium disabled:opacity-50"
            style={{
              backgroundColor: "var(--color-surface-tertiary)",
              color: "var(--color-text-secondary)",
            }}
          >
            {cancelLabel}
          </button>
          <button
            type="button"
            onClick={onConfirm}
            disabled={busy}
            className="rounded-md px-4 py-2 text-sm font-medium disabled:opacity-50"
            style={{
              backgroundColor: danger ? "var(--color-danger)" : "var(--color-primary)",
              color: "var(--color-text-inverse)",
            }}
          >
            {busy ? (
              <span className="inline-flex items-center gap-2">
                <Spinner size="sm" />
                Working...
              </span>
            ) : (
              confirmLabel
            )}
          </button>
        </div>
      </div>
    </div>
  );
}
