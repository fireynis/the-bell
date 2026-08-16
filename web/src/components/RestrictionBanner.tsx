import { useCallback, useEffect, useState } from "react";
import { moderationApi } from "../api/client.ts";
import type { ApiError, MuteStatus, SuspensionStatus } from "../api/types.ts";
import {
  activeMuteExpiry,
  activeSuspensionExpiry,
  liftRestrictionBlockReason,
  restrictionCopy,
  restrictionHeadline,
  restrictionReadFailure,
  type RestrictionKind,
} from "../lib/moderation.ts";
import ConfirmDialog from "./ConfirmDialog.tsx";
import ErrorBanner from "./ErrorBanner.tsx";

/**
 * One time-boxed restriction as this banner deals with it: how to read whether
 * it is in force, and how to clear it.
 *
 * Mirrors the `restriction` descriptor in
 * internal/service/moderation_action.go, which exists so the mute and the
 * suspension share one implementation of reading and lifting rather than two
 * that can drift. The same argument holds on this side of the wire: the
 * idempotent 204, the self-lift refusal, the reset when the page changes user
 * and the refusal to infer state from the history are each one rule, and two
 * hand-written banners would be two chances for one of them to be got wrong.
 */
export interface Restriction<S> {
  kind: RestrictionKind;
  /** The live read. Never the action history — see the comment on the component. */
  read: (userId: string) => Promise<S>;
  /** The lift. Answers 204 whether or not the restriction was in force. */
  lift: (userId: string) => Promise<unknown>;
  /** When it ends, or null when nothing is in force at `now`. */
  activeExpiry: (status: S | null, now: Date) => Date | null;
}

const MUTE_RESTRICTION: Restriction<MuteStatus> = {
  kind: "mute",
  read: (userId) => moderationApi.getMuteStatus(userId),
  lift: (userId) => moderationApi.liftMute(userId),
  activeExpiry: activeMuteExpiry,
};

const SUSPENSION_RESTRICTION: Restriction<SuspensionStatus> = {
  kind: "suspension",
  read: (userId) => moderationApi.getSuspensionStatus(userId),
  lift: (userId) => moderationApi.liftSuspension(userId),
  activeExpiry: activeSuspensionExpiry,
};

export interface BannerProps {
  /** The user whose history is on screen. */
  userId: string;
  /** The signed-in moderator, who may not lift a restriction placed on themselves. */
  viewerId: string;
}

interface RestrictionBannerProps<S> extends BannerProps {
  restriction: Restriction<S>;
}

/** The mute banner: the live mute, and the only way to end one early. */
export function MuteBanner(props: BannerProps) {
  return <RestrictionBanner restriction={MUTE_RESTRICTION} {...props} />;
}

/** The suspension banner, which is the mute's one severity up. */
export function SuspensionBanner(props: BannerProps) {
  return <RestrictionBanner restriction={SUSPENSION_RESTRICTION} {...props} />;
}

/**
 * RestrictionBanner is where a moderator learns that the person they are
 * looking at is muted or suspended right now, and the only place they can end
 * either early.
 *
 * It reads the live endpoint — GET /moderation/users/{id}/mute or
 * .../suspension — rather than inferring the restriction from the action
 * history below it. The history is immutable: a mute or suspend action keeps
 * the expiry the moderator gave it, forever, so a page that inferred "suspended
 * until X" from it would keep saying so after the suspension was lifted, and
 * after a reload too. The live read is the only thing that can be right in both
 * directions.
 *
 * A failed read says so instead of rendering nothing: silence here would read
 * as "not restricted", and neither muted_until nor suspended_until is on any
 * other response a moderator can see. (`is_active` is not a fallback for the
 * suspension — it is false for a plainly deactivated account too, and never
 * says when the suspension ends.)
 */
export default function RestrictionBanner<S>({
  restriction,
  userId,
  viewerId,
}: RestrictionBannerProps<S>) {
  const [status, setStatus] = useState<S | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [confirming, setConfirming] = useState(false);
  const [lifting, setLifting] = useState(false);
  const [liftError, setLiftError] = useState<string | null>(null);
  const [lifted, setLifted] = useState(false);

  const copy = restrictionCopy(restriction.kind);

  const load = useCallback(async () => {
    setLoadError(null);
    try {
      setStatus(await restriction.read(userId));
    } catch (err) {
      setStatus(null);
      setLoadError((err as ApiError).error ?? "");
    }
  }, [restriction, userId]);

  useEffect(() => {
    // Reset on a change of user, so one member's restriction is never shown
    // over another's history while the new read is in flight.
    setLifted(false);
    setLiftError(null);
    setConfirming(false);
    void load();
  }, [load]);

  // `lifted` overrides the loaded status rather than overwriting it: the
  // endpoint answers 204 with no body, so there is nothing to write back, and
  // the server's answer to "is this person restricted" is now definitively no.
  // Re-reading to learn what we already know would only add a way to fail.
  const expiry = lifted ? null : restriction.activeExpiry(status, new Date());
  const blockReason = liftRestrictionBlockReason(viewerId, userId);

  async function handleLift() {
    if (lifting) return;
    setLifting(true);
    setLiftError(null);
    try {
      await restriction.lift(userId);
      setLifted(true);
      setConfirming(false);
    } catch (err) {
      // The restriction still stands, so the banner and the dialog both stay.
      // Closing either would tell the moderator the appeal was handled while
      // the member is still restricted.
      setLiftError((err as ApiError).error ?? copy.liftFailure);
    } finally {
      setLifting(false);
    }
  }

  if (loadError) {
    return (
      <div className="mb-4">
        <ErrorBanner message={restrictionReadFailure(restriction.kind, loadError)} onRetry={load} />
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
        {copy.liftedNotice}
      </div>
    );
  }

  return (
    <div
      className="mb-4 flex flex-wrap items-center justify-between gap-3 rounded-[var(--radius-md)] p-3"
      style={{ backgroundColor: "var(--color-accent-light)", color: "var(--color-accent)" }}
    >
      <div className="text-sm">
        <p className="font-semibold">{restrictionHeadline(restriction.kind, expiry)}</p>
        {blockReason && <p className="mt-1 text-xs">{blockReason}</p>}
      </div>

      <button
        type="button"
        onClick={() => setConfirming(true)}
        disabled={blockReason !== null}
        className="rounded-md px-3 py-1.5 text-sm font-medium disabled:opacity-50"
        style={{ backgroundColor: "var(--color-surface)", color: "var(--color-text)" }}
      >
        {copy.liftLabel}
      </button>

      {confirming && (
        <ConfirmDialog
          title={copy.confirmTitle}
          body={copy.confirmBody}
          confirmLabel={copy.liftLabel}
          busy={lifting}
          error={liftError}
          onConfirm={handleLift}
          onCancel={() => {
            setConfirming(false);
            setLiftError(null);
          }}
        />
      )}
    </div>
  );
}
