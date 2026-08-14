import { useState } from "react";
import { vouchApi } from "../../api/client";
import type { User, Vouch } from "../../api/types";
import ConfirmDialog from "../../components/ConfirmDialog";
import Spinner from "../../components/Spinner";
import {
  canRevokeVouch,
  describeRevokeCost,
  findActiveVouch,
  vouchBlockReason,
} from "../../lib/vouch";
import { apiErrorMessage } from "../../lib/verification";

interface VouchActionProps {
  /** The signed-in viewer, or null when nobody is. */
  viewer: User | null;
  /** The profile being viewed. */
  target: User;
  /** Vouches the target has received, used to spot the viewer's own. */
  received: Vouch[];
  /** Vouches the viewer has given, used for the duplicate and daily-limit checks. */
  given: Vouch[];
  /** Called after a successful vouch or revocation so the page can refetch. */
  onChanged: () => void;
}

/**
 * VouchAction renders the vouch or revoke control on someone else's profile.
 *
 * It renders nothing at all on the viewer's own profile: vouching for yourself
 * is refused by the server, and offering a control that can only fail is worse
 * than not offering one. Everywhere else it shows either the action or the
 * reason it is unavailable, never a disabled button with no explanation.
 */
export default function VouchAction({
  viewer,
  target,
  received,
  given,
  onChanged,
}: VouchActionProps) {
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [confirmingRevoke, setConfirmingRevoke] = useState(false);

  // Nothing to offer on your own profile, and nothing to explain either.
  if (!viewer || viewer.id === target.id) return null;

  const existing = findActiveVouch(received, viewer.id, target.id);
  const blockReason = vouchBlockReason(viewer, target.id, given);
  const targetName = target.display_name || target.id.slice(0, 8);

  async function submit(action: () => Promise<unknown>) {
    setSubmitting(true);
    setError(null);
    try {
      await action();
      setConfirmingRevoke(false);
      onChanged();
    } catch (err) {
      // The server refuses for reasons the client cannot predict — a cycle in
      // the trust graph, the vouchee's state, a limit counted over rows this
      // client never sees. Its message is the only accurate one available,
      // bar the verification guard, whose three words are a state rather than
      // something the viewer could act on.
      setError(apiErrorMessage(err, "That did not work. Please try again."));
    } finally {
      setSubmitting(false);
    }
  }

  const vouch = () => submit(() => vouchApi.create({ vouchee_id: target.id }));
  const revoke = () => existing && submit(() => vouchApi.revoke(existing.id));

  if (existing) {
    const revocable = canRevokeVouch(existing, viewer);

    return (
      <div className="mt-4">
        <p className="mb-2 text-sm" style={{ color: "var(--color-text-secondary)" }}>
          You have vouched for {targetName}.
        </p>

        {revocable && (
          <button
            type="button"
            onClick={() => {
              setError(null);
              setConfirmingRevoke(true);
            }}
            className="rounded-md px-4 py-2 text-sm font-medium"
            style={{
              backgroundColor: "var(--color-surface-tertiary)",
              color: "var(--color-danger)",
            }}
          >
            Revoke vouch
          </button>
        )}

        {/* The error is shown here too, since the dialog closes on success but
            stays open on failure — a revocation that failed after the dialog
            was dismissed would otherwise vanish without a word. */}
        {error && !confirmingRevoke && (
          <p className="mt-2 text-sm" style={{ color: "var(--color-danger)" }}>
            {error}
          </p>
        )}

        {confirmingRevoke && (
          <ConfirmDialog
            title={`Revoke your vouch for ${targetName}?`}
            body={describeRevokeCost(targetName)}
            confirmLabel="Revoke vouch"
            danger
            busy={submitting}
            error={error}
            onConfirm={revoke}
            onCancel={() => {
              setConfirmingRevoke(false);
              setError(null);
            }}
          />
        )}
      </div>
    );
  }

  if (blockReason) {
    return (
      <p className="mt-4 text-sm" style={{ color: "var(--color-text-tertiary)" }}>
        {blockReason}
      </p>
    );
  }

  return (
    <div className="mt-4">
      <button
        type="button"
        onClick={vouch}
        disabled={submitting}
        className="rounded-md px-4 py-2 text-sm font-medium disabled:opacity-50"
        style={{
          backgroundColor: "var(--color-primary)",
          color: "var(--color-text-inverse)",
        }}
      >
        {submitting ? (
          <span className="inline-flex items-center gap-2">
            <Spinner size="sm" />
            Vouching...
          </span>
        ) : (
          `Vouch for ${targetName}`
        )}
      </button>

      <p className="mt-2 text-xs" style={{ color: "var(--color-text-tertiary)" }}>
        Vouching raises {targetName}'s trust score and stakes some of your own standing on them.
      </p>

      {error && (
        <p className="mt-2 text-sm" style={{ color: "var(--color-danger)" }}>
          {error}
        </p>
      )}
    </div>
  );
}
