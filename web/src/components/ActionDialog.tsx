import { useState, useEffect } from "react";
import { moderationApi } from "../api/client.ts";
import type { User } from "../api/types.ts";
import ErrorBanner from "./ErrorBanner.tsx";
import { useModalDialog } from "../hooks/useModalDialog.ts";
import {
  ACTION_TYPES,
  MAX_ACTION_REASON_LENGTH,
  MAX_DURATION_HOURS,
  actionBlockReason,
  actionConsequence,
  buildActionRequest,
  needsDuration as actionNeedsDuration,
  severitiesFor,
  severityChoiceLabel,
  validateAction,
} from "../lib/moderation.ts";
import { apiErrorMessage } from "../lib/verification.ts";

/**
 * Every control in here shares the app's field styling, focus ring included.
 * That ring used to be four copies of a JS onFocus/onBlur pair writing inline
 * styles, which no keyboard-only path and no :focus-visible could reach.
 */
const FIELD_CLASS = "field block w-full rounded-[var(--radius-md)] px-3 py-2";

interface ActionDialogProps {
  targetUserId: string;
  /**
   * The signed-in moderator, or null while their own profile has not loaded.
   *
   * Their id is here because the server refuses an action somebody takes on
   * themselves, which the dialog would otherwise submit as a guaranteed 400 and
   * report as a server failure. Their role is here because a ban is the
   * council's alone — see actionBlockReason — and without it the dialog offered
   * every moderator the one irreversible action in the system, then let them
   * fill in the form and answered it with a bare "forbidden".
   */
  moderator: Pick<User, "id" | "role" | "is_active"> | null;
  onClose: () => void;
  onActionTaken: () => void;
}

export default function ActionDialog({
  targetUserId,
  moderator,
  onClose,
  onActionTaken,
}: ActionDialogProps) {
  const [actionType, setActionType] = useState("");
  const [severity, setSeverity] = useState(0);
  const [reason, setReason] = useState("");
  // Held as text, not a number, so an emptied field stays empty. Reading
  // Number(e.target.value) into number state turned a cleared box into "0",
  // which the moderator then had to select and overwrite rather than type into.
  const [durationText, setDurationText] = useState("24");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Auto-set severity when action type changes
  useEffect(() => {
    if (actionType) {
      const allowed = severitiesFor(actionType);
      if (allowed.length > 0) {
        setSeverity(allowed[0]);
      }
    }
  }, [actionType]);

  // Escape is ignored while the action is in flight: the request has gone, and
  // closing would only hide what it answered. Matches ConfirmDialog.
  const panelRef = useModalDialog<HTMLDivElement>(() => {
    if (!submitting) onClose();
  });

  const needsDuration = actionNeedsDuration(actionType);
  const choices = severitiesFor(actionType);
  // Severity is decided by the action type everywhere except a warning, which
  // may be minor or moderate. Offering a one-option dropdown for the other
  // three asked the moderator to make a choice that had already been made, and
  // said nothing about what the number meant either way — the consequences
  // below are that answer, and they are what changes when this does.
  const severityIsAChoice = choices.length > 1;
  const consequence = actionConsequence(actionType, severity);
  // An empty box is NaN rather than 0, so validateAction reports it as a
  // missing duration instead of an out-of-range one.
  const durationHours = durationText.trim() === "" ? Number.NaN : Number(durationText);
  const input = { actionType, severity, reason, moderatorId: moderator?.id ?? "", targetUserId };
  const check = validateAction(input, durationHours);
  // Checked as well as offering only the actions this moderator may take: the
  // rank rule and the form rules are separate refusals, and the select is a
  // presentation of the first rather than the enforcement of it.
  const rankBlock = actionBlockReason(actionType, moderator);
  // Stated whatever is selected, so the rule is visible before a moderator goes
  // looking for the action that is missing from their list.
  const banBlock = actionBlockReason("ban", moderator);
  const canSubmit = check.valid && rankBlock === null && !submitting;

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (submitting) return;
    if (!check.valid) {
      setError(check.error ?? null);
      return;
    }

    if (rankBlock) {
      setError(rankBlock);
      return;
    }

    setSubmitting(true);
    setError(null);

    try {
      await moderationApi.takeAction(buildActionRequest(input, durationHours));
      onActionTaken();
    } catch (err) {
      setError(apiErrorMessage(err, "Failed to take action."));
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4"
      role="dialog"
      aria-modal="true"
      aria-label="Take moderation action"
    >
      <div
        ref={panelRef}
        className="max-h-full w-full max-w-md overflow-y-auto p-6"
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
              className={FIELD_CLASS}
              aria-describedby={banBlock ? "action-type-rank" : undefined}
            >
              <option value="">Select action...</option>
              {/*
                Disabled rather than dropped for an action this moderator's rank
                does not reach. Hiding Ban would leave a moderator wondering
                where it went, or believing the town has no such power; showing
                it greyed out with the rule underneath says who does have it,
                which is the thing they need to know in order to escalate.
              */}
              {ACTION_TYPES.map((type) => {
                const blocked = actionBlockReason(type, moderator) !== null;
                const name = type.charAt(0).toUpperCase() + type.slice(1);
                return (
                  <option key={type} value={type} disabled={blocked}>
                    {blocked ? `${name} (council only)` : name}
                  </option>
                );
              })}
            </select>
            {banBlock && (
              <p
                id="action-type-rank"
                className="mt-1 text-xs"
                style={{ color: "var(--color-text-tertiary)" }}
              >
                {banBlock}
              </p>
            )}
          </div>

          {/* Severity — a warning only; see severityIsAChoice above. */}
          {severityIsAChoice && (
            <div>
              <label
                htmlFor="severity"
                className="mb-1 block text-sm font-medium"
                style={{ color: "var(--color-text-secondary)" }}
              >
                How serious?
              </label>
              <select
                id="severity"
                value={severity}
                onChange={(e) => setSeverity(Number(e.target.value))}
                className={FIELD_CLASS}
              >
                {choices.map((s) => (
                  <option key={s} value={s}>
                    {severityChoiceLabel(s)}
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
                max={MAX_DURATION_HOURS}
                value={durationText}
                onChange={(e) => setDurationText(e.target.value)}
                className={FIELD_CLASS}
              />
            </div>
          )}

          {/*
            What the action will actually do, stated before the moderator
            commits to it. The propagation line is the half nothing in this
            interface used to admit: a moderation action takes trust from the
            people who vouched for the target as well as from the target, and
            somebody deciding between a warning and a ban is entitled to know
            how many neighbours each one reaches.

            aria-live so the description follows the choice for a screen reader
            rather than sitting silently below it.
          */}
          {consequence && (
            <div
              className="rounded-[var(--radius-md)] p-3 text-sm leading-relaxed"
              style={{
                backgroundColor: "var(--color-surface-secondary)",
                color: "var(--color-text-secondary)",
              }}
              role="status"
              aria-live="polite"
              data-testid="action-consequence"
            >
              <p className="font-semibold" style={{ color: "var(--color-text)" }}>
                What {consequence.noun} does
              </p>
              <p className="mt-1">{consequence.effect}</p>
              <p className="mt-1">{consequence.penalty}</p>
              <p className="mt-2" style={{ color: "var(--color-text)" }}>
                {consequence.propagation}
              </p>
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
              maxLength={MAX_ACTION_REASON_LENGTH}
              rows={3}
              className={`${FIELD_CLASS} resize-none`}
              placeholder="Describe why this action is being taken..."
            />
            <p className="mt-1 text-xs" style={{ color: "var(--color-text-tertiary)" }}>
              {reason.length}/{MAX_ACTION_REASON_LENGTH}
            </p>
          </div>

          {error && (
            <ErrorBanner message={error} />
          )}

          {/* Why the submit button is disabled. It used to say nothing at all,
              so clearing the duration field left a dead button and no clue —
              and a moderator who opened this on their own account got no
              warning until the server answered 400. */}
          {!error && (check.error ?? rankBlock) && (
            <p className="text-sm" style={{ color: "var(--color-text-tertiary)" }} role="status">
              {check.error ?? rankBlock}
            </p>
          )}

          <div className="flex justify-end gap-3">
            <button
              type="button"
              onClick={onClose}
              className="btn btn-quiet rounded-md px-4 py-2 text-sm font-medium"
            >
              Cancel
            </button>
            <button
              type="submit"
              disabled={!canSubmit}
              className="btn btn-danger rounded-md px-4 py-2 text-sm font-medium disabled:opacity-50"
            >
              {submitting ? "Submitting..." : "Take Action"}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}
