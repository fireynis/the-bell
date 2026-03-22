import { useState, useEffect } from "react";
import { moderationApi } from "../api/client.ts";
import type { ApiError } from "../api/types.ts";
import ErrorBanner from "./ErrorBanner.tsx";

const ACTION_TYPES = ["warn", "mute", "suspend", "ban"] as const;

const SEVERITY_MAP: Record<string, number[]> = {
  warn: [1, 2],
  mute: [3],
  suspend: [4],
  ban: [5],
};

const NEEDS_DURATION = new Set(["mute", "suspend"]);

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

interface ActionDialogProps {
  targetUserId: string;
  onClose: () => void;
  onActionTaken: () => void;
}

export default function ActionDialog({
  targetUserId,
  onClose,
  onActionTaken,
}: ActionDialogProps) {
  const [actionType, setActionType] = useState("");
  const [severity, setSeverity] = useState(0);
  const [reason, setReason] = useState("");
  const [durationHours, setDurationHours] = useState(24);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [focusedField, setFocusedField] = useState<string | null>(null);

  // Auto-set severity when action type changes
  useEffect(() => {
    if (actionType) {
      const allowed = SEVERITY_MAP[actionType];
      if (allowed) {
        setSeverity(allowed[0]);
      }
    }
  }, [actionType]);

  const needsDuration = NEEDS_DURATION.has(actionType);
  const canSubmit =
    actionType !== "" &&
    severity > 0 &&
    reason.trim().length > 0 &&
    !submitting;

  function getFocusedStyle(fieldId: string): React.CSSProperties {
    return focusedField === fieldId
      ? { ...inputStyle, borderColor: "var(--color-primary)", boxShadow: `0 0 0 1px var(--color-primary)` }
      : inputStyle;
  }

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (!canSubmit) return;

    setSubmitting(true);
    setError(null);

    try {
      await moderationApi.takeAction({
        target_user_id: targetUserId,
        action_type: actionType,
        severity,
        reason: reason.trim(),
        duration_seconds: needsDuration
          ? durationHours * 3600
          : undefined,
      });
      onActionTaken();
    } catch (err) {
      const apiErr = err as ApiError;
      setError(apiErr.error ?? "Failed to take action.");
    } finally {
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
        <h2
          className="mb-4 text-lg font-semibold"
          style={{ color: "var(--color-text)" }}
        >
          Take Moderation Action
        </h2>

        <form onSubmit={handleSubmit} className="flex flex-col gap-4">
          {/* Action Type */}
          <div>
            <label
              htmlFor="action-type"
              className="mb-1 block text-sm font-medium"
              style={{ color: "var(--color-text-secondary)" }}
            >
              Action Type
            </label>
            <select
              id="action-type"
              value={actionType}
              onChange={(e) => setActionType(e.target.value)}
              style={getFocusedStyle("action-type")}
              onFocus={() => setFocusedField("action-type")}
              onBlur={() => setFocusedField(null)}
            >
              <option value="">Select action...</option>
              {ACTION_TYPES.map((type) => (
                <option key={type} value={type}>
                  {type.charAt(0).toUpperCase() + type.slice(1)}
                </option>
              ))}
            </select>
          </div>

          {/* Severity */}
          {actionType && (
            <div>
              <label
                htmlFor="severity"
                className="mb-1 block text-sm font-medium"
                style={{ color: "var(--color-text-secondary)" }}
              >
                Severity
              </label>
              <select
                id="severity"
                value={severity}
                onChange={(e) => setSeverity(Number(e.target.value))}
                style={getFocusedStyle("severity")}
                onFocus={() => setFocusedField("severity")}
                onBlur={() => setFocusedField(null)}
              >
                {SEVERITY_MAP[actionType]?.map((s) => (
                  <option key={s} value={s}>
                    Level {s}
                  </option>
                ))}
              </select>
            </div>
          )}

          {/* Duration (mute/suspend only) */}
          {needsDuration && (
            <div>
              <label
                htmlFor="duration"
                className="mb-1 block text-sm font-medium"
                style={{ color: "var(--color-text-secondary)" }}
              >
                Duration (hours)
              </label>
              <input
                id="duration"
                type="number"
                min={1}
                max={8760}
                value={durationHours}
                onChange={(e) => setDurationHours(Number(e.target.value))}
                style={getFocusedStyle("duration")}
                onFocus={() => setFocusedField("duration")}
                onBlur={() => setFocusedField(null)}
              />
            </div>
          )}

          {/* Reason */}
          <div>
            <label
              htmlFor="reason"
              className="mb-1 block text-sm font-medium"
              style={{ color: "var(--color-text-secondary)" }}
            >
              Reason
            </label>
            <textarea
              id="reason"
              value={reason}
              onChange={(e) => setReason(e.target.value)}
              maxLength={1000}
              rows={3}
              style={{ ...getFocusedStyle("reason"), resize: "none" }}
              onFocus={() => setFocusedField("reason")}
              onBlur={() => setFocusedField(null)}
              placeholder="Describe why this action is being taken..."
            />
            <p className="mt-1 text-xs" style={{ color: "var(--color-text-tertiary)" }}>
              {reason.length}/1000
            </p>
          </div>

          {error && (
            <ErrorBanner message={error} />
          )}

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
              {submitting ? "Submitting..." : "Take Action"}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}
