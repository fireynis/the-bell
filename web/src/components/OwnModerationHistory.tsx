import { useCallback, useEffect, useState } from "react";
import { userApi } from "../api/client";
import type { ApiError, OwnModerationEntry } from "../api/types";
import { describeOwnAction, OWN_HISTORY_EMPTY } from "../lib/ownHistory";
import ErrorBanner from "./ErrorBanner";
import Spinner from "./Spinner";

/**
 * How many entries one read asks for. The server's default is the same, and it
 * is far more than anybody will have: a member with twenty moderation actions
 * against them has not been a member for long.
 */
export const OWN_HISTORY_PAGE_SIZE = 20;

/**
 * OwnModerationHistory is where a member reads what moderation has been taken
 * against them: what was done, the reason as it was written, when it ends, and
 * what it cost their standing.
 *
 * Before this the answer was almost nothing. `muted_until` said a member could
 * not post but never why; a lifted mute or suspension showed up on their
 * profile; everything else — the warnings, the reasons, the trust penalties —
 * sat behind the moderator-only /v1/moderation routes. Somebody warned for
 * something learned of it only if a moderator told them out of band.
 *
 * It fetches for itself rather than being handed data, so the profile page pays
 * for the read only when a member opens the tab, and a member who never opens
 * it never makes the request.
 *
 * No moderator is named, because the response names none. That is a deliberate
 * platform rule rather than an omission here: in a town small enough to run
 * this, naming the individual turns a moderation decision into a grievance with
 * a neighbour.
 */
export default function OwnModerationHistory() {
  const [entries, setEntries] = useState<OwnModerationEntry[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const data = await userApi.ownModerationHistory(OWN_HISTORY_PAGE_SIZE, 0);
      setEntries(data.actions ?? []);
    } catch (err) {
      const apiErr = err as ApiError;
      setError(apiErr.error ?? "Could not load your moderation history.");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  if (loading) {
    return (
      <div className="flex justify-center py-8">
        <Spinner />
      </div>
    );
  }

  if (error) {
    return <ErrorBanner message={error} onRetry={load} />;
  }

  if (entries.length === 0) {
    return (
      <p className="text-sm" style={{ color: "var(--color-text-tertiary)" }}>
        {OWN_HISTORY_EMPTY}
      </p>
    );
  }

  // Read once for the whole list so every entry is described against the same
  // clock; entries rendered a millisecond apart should not disagree about
  // whether a restriction has ended.
  const now = new Date();

  return (
    <div className="flex flex-col gap-4" data-testid="own-moderation-history">
      {entries.map((entry) => (
        <OwnHistoryEntry key={entry.id} entry={entry} now={now} />
      ))}

      {/*
        Said only when the page came back full, which is the only case where
        there might be more. Promising a complete record and quietly truncating
        it would be worse than showing nothing at all.
      */}
      {entries.length === OWN_HISTORY_PAGE_SIZE && (
        <p className="text-xs" style={{ color: "var(--color-text-tertiary)" }}>
          Showing your {OWN_HISTORY_PAGE_SIZE} most recent.
        </p>
      )}
    </div>
  );
}

interface OwnHistoryEntryProps {
  entry: OwnModerationEntry;
  now: Date;
}

/**
 * One entry, read top to bottom as a short paragraph: what happened, when, why,
 * how long it lasts, and what it cost.
 *
 * There is deliberately no severity badge and no coloured chip. The moderator's
 * ActionHistoryCard has both, because a moderator is triaging a stranger's
 * record at a glance; the member reading their own is not triaging anything,
 * and a red badge on their own profile is a punishment repeated rather than
 * information.
 */
function OwnHistoryEntry({ entry, now }: OwnHistoryEntryProps) {
  const summary = describeOwnAction(entry, now);

  return (
    <article
      className="rounded-lg p-4"
      style={{
        backgroundColor: "var(--color-surface)",
        border: "1px solid var(--color-border-light)",
        borderRadius: "var(--radius-md)",
      }}
    >
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <h3 className="text-sm font-semibold" style={{ color: "var(--color-text)" }}>
          {summary.headline}
        </h3>
        {summary.when && (
          <span className="text-xs" style={{ color: "var(--color-text-tertiary)" }}>
            {summary.when}
          </span>
        )}
      </div>

      {summary.reason && (
        <p className="mt-2 text-sm" style={{ color: "var(--color-text)" }}>
          {summary.reason}
        </p>
      )}

      {summary.restriction && (
        <p className="mt-2 text-sm" style={{ color: "var(--color-text-secondary)" }}>
          {summary.restriction}
        </p>
      )}

      {summary.cost && (
        <p className="mt-1 text-sm" style={{ color: "var(--color-text-secondary)" }}>
          {summary.cost}
        </p>
      )}
    </article>
  );
}
