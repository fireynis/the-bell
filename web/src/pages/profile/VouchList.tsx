import { useState } from "react";
import { Link } from "react-router";
import { vouchApi } from "../../api/client";
import type { ApiError, User, Vouch } from "../../api/types";
import ConfirmDialog from "../../components/ConfirmDialog";
import {
  REVOKE_PENALTY_ENFORCED,
  canRevokeVouch,
  describeRevokeCost,
} from "../../lib/vouch";

/** Which side of the vouch relationship this list is showing. */
export type VouchDirection = "received" | "given";

const HEADINGS: Record<VouchDirection, string> = {
  received: "Received",
  given: "Given",
};

/**
 * counterpartId returns the other party in a vouch: whoever vouched for this
 * profile in a received vouch, and whoever this profile vouched for in a given
 * one. Listing the profile's own id would make every row link back to the page
 * it is already on.
 */
function counterpartId(vouch: Vouch, direction: VouchDirection): string {
  return direction === "received" ? vouch.voucher_id : vouch.vouchee_id;
}

/** How the list labels a member it knows only by id. */
function shortId(userId: string): string {
  return `${userId.slice(0, 8)}...`;
}

/**
 * VouchList renders one side of a profile's vouches.
 *
 * The direction is a prop rather than something inferred from the heading. It
 * used to be derived by comparing the heading text to the literal "Received",
 * so renaming the heading — a change any designer would consider cosmetic —
 * would have flipped every row to link and label the wrong user, with the list
 * still rendering perfectly and nothing failing.
 *
 * Rows carry a revoke control wherever canRevokeVouch allows one, which is what
 * VouchService.Revoke allows: the voucher, and any moderator or council member.
 * Revoke used to be offered only by VouchAction, and only to the voucher on the
 * profile of the person they had vouched for — so a moderator clearing out a
 * ring of bad endorsements had to open each vouchee's profile in turn, and
 * could not act from the list that shows them the ring.
 */
export default function VouchList({
  direction,
  vouches,
  viewer,
  ownerName,
  onRevoked,
}: {
  direction: VouchDirection;
  vouches: Vouch[];
  /** The signed-in viewer, or null when nobody is; decides who may revoke. */
  viewer: User | null;
  /** Display name of the profile these vouches belong to, for the warning. */
  ownerName?: string;
  /** Called after a successful revocation so the page can refetch. */
  onRevoked: () => void;
}) {
  const heading = HEADINGS[direction];
  const [confirming, setConfirming] = useState<Vouch | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function revoke(vouch: Vouch) {
    setSubmitting(true);
    setError(null);
    try {
      await vouchApi.revoke(vouch.id);
      setConfirming(null);
      onRevoked();
    } catch (err) {
      // The server refuses for reasons this list cannot see — the vouch already
      // revoked by someone else, the viewer's role changed since the page
      // loaded. Its message is the only accurate one available.
      const apiErr = err as ApiError;
      setError(apiErr.error ?? "That did not work. Please try again.");
    } finally {
      setSubmitting(false);
    }
  }

  // Both parties by name where the page knows one: the profile owner is the
  // vouchee of a received vouch and the voucher of a given one.
  function namesFor(vouch: Vouch): { voucherName: string; voucheeName: string } {
    const owner = ownerName?.trim() ? ownerName.trim() : shortId(counterpartId(vouch, direction));
    return direction === "received"
      ? { voucherName: shortId(vouch.voucher_id), voucheeName: owner }
      : { voucherName: owner, voucheeName: shortId(vouch.vouchee_id) };
  }

  /**
   * Only the voucher pays the revocation penalty, so the warning a moderator
   * sees must not quote it — VouchService.Revoke charges nobody else.
   */
  function warningFor(vouch: Vouch): string {
    const { voucherName, voucheeName } = namesFor(vouch);
    const ownVouch = vouch.voucher_id === viewer?.id;
    return describeRevokeCost(
      voucheeName,
      ownVouch && REVOKE_PENALTY_ENFORCED,
      ownVouch ? undefined : voucherName,
    );
  }

  return (
    <div>
      <h3
        className="mb-2 text-sm font-medium"
        style={{ color: "var(--color-text-secondary)" }}
      >
        {heading}
      </h3>
      {vouches.length === 0 ? (
        <p className="text-sm" style={{ color: "var(--color-text-tertiary)" }}>
          None yet.
        </p>
      ) : (
        <ul className="space-y-2">
          {vouches.map((v) => {
            const otherId = counterpartId(v, direction);
            return (
              <li
                key={v.id}
                className="rounded-md p-3"
                style={{
                  backgroundColor: "var(--color-surface)",
                  boxShadow: "var(--shadow-sm)",
                }}
              >
                <div className="flex items-center justify-between">
                  <Link
                    to={`/profile/${otherId}`}
                    className="text-sm font-medium"
                    style={{ color: "var(--color-primary)" }}
                  >
                    {shortId(otherId)}
                  </Link>
                  <div className="flex items-center gap-3">
                    <span
                      className="text-xs"
                      style={{ color: "var(--color-text-tertiary)" }}
                    >
                      {new Date(v.created_at).toLocaleDateString()}
                    </span>
                    {canRevokeVouch(v, viewer) && (
                      <button
                        type="button"
                        onClick={() => {
                          setError(null);
                          setConfirming(v);
                        }}
                        className="rounded-md px-2 py-1 text-xs font-medium"
                        style={{
                          backgroundColor: "var(--color-surface-tertiary)",
                          color: "var(--color-danger)",
                        }}
                      >
                        Revoke
                      </button>
                    )}
                  </div>
                </div>
              </li>
            );
          })}
        </ul>
      )}

      {/* The dialog closes on success but stays open on failure, so a
          revocation that failed after it was dismissed would otherwise vanish
          without a word. */}
      {error && !confirming && (
        <p className="mt-2 text-sm" style={{ color: "var(--color-danger)" }}>
          {error}
        </p>
      )}

      {confirming && (
        <ConfirmDialog
          title={
            confirming.voucher_id === viewer?.id
              ? `Revoke your vouch for ${namesFor(confirming).voucheeName}?`
              : `Revoke ${namesFor(confirming).voucherName}'s vouch?`
          }
          body={warningFor(confirming)}
          confirmLabel="Revoke vouch"
          danger
          busy={submitting}
          error={error}
          onConfirm={() => revoke(confirming)}
          onCancel={() => {
            setConfirming(null);
            setError(null);
          }}
        />
      )}
    </div>
  );
}
