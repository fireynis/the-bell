import type { User } from "../api/types";
import { ownMuteNotice } from "../lib/moderation";
import { formatDateTime } from "../lib/time";

interface OwnMuteNoticeProps {
  /** The signed-in member's own profile. Another member's carries no mute state. */
  user: User;
}

/**
 * OwnMuteNotice is where a member learns about their own mute, and the only
 * place they learn that one was lifted early.
 *
 * The lift half is the reason this exists. muted_until vanishes the moment a
 * moderator releases someone, so before it a member's profile simply stopped
 * saying they were muted — no record, no explanation, and nothing to check.
 * The member-facing history at GET /api/v1/users/me/moderation-history does
 * not cover it either: a lift writes no action row, so this notice stays the
 * one place an early release is visible to the person released.
 *
 * It renders nothing at all when there is neither a mute nor a lift, rather
 * than an empty panel: a member who has never been moderated should not be
 * shown a moderation section of their profile.
 *
 * No moderator is named. Which moderator acted is on no member-facing response,
 * and deciding that members may see it is a policy question rather than a
 * detail of this notice.
 */
export default function OwnMuteNotice({ user }: OwnMuteNoticeProps) {
  const notice = ownMuteNotice(user, new Date());
  if (!notice.hasAnything) return null;

  const { mutedUntil, latestLift } = notice;

  return (
    <div
      className="mt-4 rounded-lg p-4 text-sm"
      style={{
        backgroundColor: "var(--color-surface-alt, var(--color-surface))",
        border: "1px solid var(--color-border)",
        borderRadius: "var(--radius-md)",
      }}
      data-testid="own-mute-notice"
    >
      {mutedUntil && (
        <p style={{ color: "var(--color-text)" }}>
          <strong>You are muted until {formatDateTime(mutedUntil.toISOString())}.</strong>{" "}
          You cannot post until then. Your other activity is unaffected.
        </p>
      )}

      {latestLift && (
        <p
          className={mutedUntil ? "mt-2" : undefined}
          style={{ color: "var(--color-text-secondary)" }}
        >
          A moderator lifted a mute on your account on{" "}
          {formatDateTime(latestLift.lifted_at)}
          {latestLift.previous_muted_until
            ? `, which would otherwise have run until ${formatDateTime(latestLift.previous_muted_until)}.`
            : "."}
        </p>
      )}
    </div>
  );
}
