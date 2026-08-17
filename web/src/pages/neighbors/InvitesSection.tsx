import { useCallback, useEffect, useState } from "react";
import { inviteApi } from "../../api/client";
import type { ApiError, Invite } from "../../api/types";
import ConfirmDialog from "../../components/ConfirmDialog";
import ErrorBanner from "../../components/ErrorBanner";
import Spinner from "../../components/Spinner";
import {
  INVITES_SECTION,
  canRevokeInvite,
  describeInviteCount,
  describeInviteStatus,
  inviteErrorMessage,
} from "../../lib/invite";

interface InvitesSectionProps {
  /**
   * Bumped by the page when an invitation has just been sent, so the list picks
   * it up without the section having to be told what was created.
   */
  reloadToken: number;
}

/**
 * InvitesSection is the sender's record of who they have invited.
 *
 * Collapsed by default, with the count of what is still out in the header. The
 * Neighbours page is for finding a neighbour, and a list of invitations —
 * which accumulates accepted and expired ones forever — would push the
 * directory down the page for the sake of something people consult
 * occasionally. The count is what makes the collapsed state honest: you can see
 * whether there is anything worth opening without opening it.
 *
 * Revoking is behind a confirm dialog because the link stops working the
 * instant it happens and the person holding it is given no warning. Only open
 * invitations offer it — see canRevokeInvite for why an accepted one does not.
 */
export default function InvitesSection({ reloadToken }: InvitesSectionProps) {
  const [invites, setInvites] = useState<Invite[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [expanded, setExpanded] = useState(false);
  const [revoking, setRevoking] = useState<Invite | null>(null);
  const [revokeBusy, setRevokeBusy] = useState(false);
  const [revokeError, setRevokeError] = useState<string | null>(null);
  // Read once per load rather than per render, so the countdowns cannot shift
  // mid-render and the list stays a pure function of what was fetched.
  const [now, setNow] = useState(() => Date.now());

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const data = await inviteApi.list();
      setInvites(Array.isArray(data?.invites) ? data.invites : []);
      setNow(Date.now());
    } catch {
      setError(INVITES_SECTION.loadError);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
  }, [load, reloadToken]);

  async function handleRevoke() {
    if (!revoking) return;
    setRevokeBusy(true);
    setRevokeError(null);
    try {
      await inviteApi.revoke(revoking.id);
      setRevoking(null);
      await load();
    } catch (err) {
      setRevokeError(inviteErrorMessage(err as ApiError, "revoke"));
    } finally {
      setRevokeBusy(false);
    }
  }

  const count = describeInviteCount(invites);
  const showEmpty = !loading && !error && invites.length === 0;

  return (
    <section
      className="mt-4 rounded-[var(--radius-lg)] p-4"
      style={{
        backgroundColor: "var(--color-surface)",
        boxShadow: "var(--shadow-sm)",
      }}
      aria-label={INVITES_SECTION.title}
    >
      <button
        type="button"
        onClick={() => setExpanded((open) => !open)}
        aria-expanded={expanded}
        className="flex w-full items-center justify-between gap-3 text-left"
      >
        <span className="text-base font-semibold" style={{ color: "var(--color-text)" }}>
          {INVITES_SECTION.title}
        </span>
        <span className="text-xs" style={{ color: "var(--color-text-tertiary)" }}>
          {count ? `${count} · ` : ""}
          {expanded ? "Hide" : "Show"}
        </span>
      </button>

      {expanded && (
        <div className="mt-3">
          {error && <ErrorBanner message={error} onRetry={load} />}

          {loading && (
            <div className="flex justify-center py-4">
              <Spinner />
            </div>
          )}

          {showEmpty && (
            <p className="text-sm" style={{ color: "var(--color-text-tertiary)" }}>
              {INVITES_SECTION.empty}
            </p>
          )}

          <ul className="flex flex-col">
            {invites.map((invite) => (
              <li
                key={invite.id}
                className="flex items-start justify-between gap-3 py-2"
                style={{ borderTop: "1px solid var(--color-border-light)" }}
              >
                <div className="min-w-0">
                  <p
                    className="truncate text-sm font-medium"
                    style={{ color: "var(--color-text)" }}
                  >
                    {invite.email}
                  </p>
                  <p className="text-xs" style={{ color: "var(--color-text-secondary)" }}>
                    {describeInviteStatus(invite, now)}
                  </p>
                  {/*
                    The sender's own note, shown back to them: it is how they
                    tell one invitation from another a week later, and it is the
                    only part of the invitation they wrote.
                  */}
                  {invite.note?.trim() && (
                    <p
                      className="mt-1 text-xs italic"
                      style={{ color: "var(--color-text-tertiary)" }}
                    >
                      &ldquo;{invite.note.trim()}&rdquo;
                    </p>
                  )}
                </div>

                {canRevokeInvite(invite) && (
                  <button
                    type="button"
                    onClick={() => {
                      setRevokeError(null);
                      setRevoking(invite);
                    }}
                    className="flex-shrink-0 rounded-md px-3 py-1 text-xs font-medium"
                    style={{
                      backgroundColor: "var(--color-surface-tertiary)",
                      color: "var(--color-text-secondary)",
                    }}
                    aria-label={`${INVITES_SECTION.revoke} the invitation to ${invite.email}`}
                  >
                    {INVITES_SECTION.revoke}
                  </button>
                )}
              </li>
            ))}
          </ul>
        </div>
      )}

      {revoking && (
        <ConfirmDialog
          title={INVITES_SECTION.revokeTitle}
          body={INVITES_SECTION.revokeBody(revoking.email)}
          confirmLabel={INVITES_SECTION.revoke}
          danger
          busy={revokeBusy}
          error={revokeError}
          onConfirm={handleRevoke}
          onCancel={() => {
            if (!revokeBusy) setRevoking(null);
          }}
        />
      )}
    </section>
  );
}
