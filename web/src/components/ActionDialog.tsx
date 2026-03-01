import { useState, useEffect } from "react";
import { moderationApi } from "../api/client.ts";
import type { ApiError } from "../api/types.ts";

const ACTION_TYPES = ["warn", "mute", "suspend", "ban"] as const;

const SEVERITY_MAP: Record<string, number[]> = {
  warn: [1, 2],
  mute: [3],
  suspend: [4],
  ban: [5],
};

const NEEDS_DURATION = new Set(["mute", "suspend"]);

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
      <div className="w-full max-w-md rounded-lg bg-white p-6 shadow-xl">
        <h2 className="mb-4 text-lg font-semibold text-gray-900">
          Take Moderation Action
        </h2>

        <form onSubmit={handleSubmit} className="flex flex-col gap-4">
          {/* Action Type */}
          <div>
            <label
              htmlFor="action-type"
              className="mb-1 block text-sm font-medium text-gray-700"
            >
              Action Type
            </label>
            <select
              id="action-type"
              value={actionType}
              onChange={(e) => setActionType(e.target.value)}
              className="block w-full rounded-md border border-gray-300 px-3 py-2 focus:border-indigo-500 focus:ring-1 focus:ring-indigo-500 focus:outline-none"
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
                className="mb-1 block text-sm font-medium text-gray-700"
              >
                Severity
              </label>
              <select
                id="severity"
                value={severity}
                onChange={(e) => setSeverity(Number(e.target.value))}
                className="block w-full rounded-md border border-gray-300 px-3 py-2 focus:border-indigo-500 focus:ring-1 focus:ring-indigo-500 focus:outline-none"
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
                className="mb-1 block text-sm font-medium text-gray-700"
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
                className="block w-full rounded-md border border-gray-300 px-3 py-2 focus:border-indigo-500 focus:ring-1 focus:ring-indigo-500 focus:outline-none"
              />
            </div>
          )}

          {/* Reason */}
          <div>
            <label
              htmlFor="reason"
              className="mb-1 block text-sm font-medium text-gray-700"
            >
              Reason
            </label>
            <textarea
              id="reason"
              value={reason}
              onChange={(e) => setReason(e.target.value)}
              maxLength={1000}
              rows={3}
              className="block w-full resize-none rounded-md border border-gray-300 px-3 py-2 focus:border-indigo-500 focus:ring-1 focus:ring-indigo-500 focus:outline-none"
              placeholder="Describe why this action is being taken..."
            />
            <p className="mt-1 text-xs text-gray-500">
              {reason.length}/1000
            </p>
          </div>

          {error && (
            <div className="rounded-md bg-red-50 p-3 text-sm text-red-700">
              {error}
            </div>
          )}

          <div className="flex justify-end gap-3">
            <button
              type="button"
              onClick={onClose}
              className="rounded-md bg-gray-100 px-4 py-2 text-sm font-medium text-gray-700 hover:bg-gray-200"
            >
              Cancel
            </button>
            <button
              type="submit"
              disabled={!canSubmit}
              className="rounded-md bg-red-600 px-4 py-2 text-sm font-medium text-white hover:bg-red-500 disabled:opacity-50"
            >
              {submitting ? "Submitting..." : "Take Action"}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}
