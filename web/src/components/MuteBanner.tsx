import { useCallback, useEffect, useState } from "react";
import { moderationApi } from "../api/client.ts";
import type { ApiError, MuteStatus } from "../api/types.ts";
import { activeMuteExpiry, liftMuteBlockReason } from "../lib/moderation.ts";
import { formatDateTime } from "../lib/time.ts";
import ErrorBanner from "./ErrorBanner.tsx";

interface MuteBannerProps {
  /** The user whose history is on screen. */
  userId: string;
  /** The signed-in moderator, who may not lift a mute placed on themselves. */
  viewerId: string;
}

/**
 * MuteBanner is where a moderator learns that the person they are looking at is
 * muted, and the only place they can end it early.
 *
 * It reads GET /moderation/users/{id}/mute rather than inferring the mute from
 * the action history below it. The history is immutable — the mute action stays
 * exactly as the moderator wrote it, expiry and all — so a page that inferred
 * "muted until X" from it would keep saying so after the mute was lifted, and
 * after a reload too. The live read is the only thing that can be right in both
 * directions.
 *
 * A failed read says so instead of rendering nothing: silence here would read
 * as "not muted", and muted_until is on no other response a moderator can see,
 * so there is nothing else to check it against.
 */
export default function MuteBanner({ userId, viewerId }: MuteBannerProps) {
  const [status, setStatus] = useState<MuteStatus | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [lifting, setLifting] = useState(false);
  const [liftError, setLiftError] = useState<string | null>(null);
  const [lifted, setLifted] = useState(false);

  const load = useCallback(async () => {
    setLoadError(null);
    try {
      setStatus(await moderationApi.getMuteStatus(userId));
    } catch (err) {
      setStatus(null);
      setLoadError((err as ApiError).error ?? "");
    }
  }, [userId]);

  useEffect(() => {
    // Reset on a change of user, so one member's mute is never shown over
    // another's history while the new read is in flight.
    setLifted(false);
    setLiftError(null);
    void load();
  }, [load]);

  const expiry = activeMuteExpiry(status, new Date());
  const blockReason = liftMuteBlockReason(viewerId, userId);

  async function handleLift() {
    if (lifting) return;
    setLifting(true);
    setLiftError(null);
    try {
      await moderationApi.liftMute(userId);
      // Cleared locally rather than re-read: the endpoint answers 204, and the
      // server's answer to "is this person muted" is now definitively no.
      setStatus({});
      setLifted(true);
    } catch (err) {
      // The mute is still in force, so the banner stays. Clearing it here would
      // tell the moderator the appeal was handled while the member is still
      // silenced.
      setLiftError((err as ApiError).error ?? "The mute could not be lifted.");
    } finally {
      setLifting(false);
    }
  }

  if (loadError) {
    return (
      <div className="mb-4">
        <ErrorBanner
          message={`This user's mute status could not be read${loadError ? `: ${loadError}` : "."}`}
          onRetry={load}
        />
      </div>
    );
  }

  if (!expiry) {
    if (!lifted) return null;
    return (
      <div
        className="mb-4 rounded-[var(--radius-md)] p-3 text-sm"
        style={{ backgroundColor: "var(--color-surface-secondary)", color: "var(--color-text-secondary)" }}
        role="status"
      >
        Mute lifted. This user can post again.
      </div>
    );
  }

  return (
    <div
      className="mb-4 flex flex-wrap items-center justify-between gap-3 rounded-[var(--radius-md)] p-3"
      style={{ backgroundColor: "var(--color-accent-light)", color: "var(--color-accent)" }}
    >
      <div className="text-sm">
        <p className="font-semibold">Currently muted until {formatDateTime(expiry.toISOString())}</p>
        {blockReason && <p className="mt-1 text-xs">{blockReason}</p>}
      </div>

      <button
        type="button"
        onClick={handleLift}
        disabled={lifting || blockReason !== null}
        className="rounded-md px-3 py-1.5 text-sm font-medium disabled:opacity-50"
        style={{ backgroundColor: "var(--color-surface)", color: "var(--color-text)" }}
      >
        {lifting ? "Lifting..." : "Lift mute"}
      </button>

      {liftError && (
        <div className="w-full">
          <ErrorBanner message={liftError} />
        </div>
      )}
    </div>
  );
}
