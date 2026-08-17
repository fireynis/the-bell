import { useState } from "react";
import { Link } from "react-router";
import { approvalApi } from "../../api/client";
import ErrorBanner from "../../components/ErrorBanner";
import Spinner from "../../components/Spinner";
import { useDebouncedValue } from "../../hooks/useDebouncedValue";
import { usePendingApprovals } from "../../hooks/usePendingApprovals";
import {
  APPROVALS_EMPTY,
  APPROVALS_NO_MATCH,
  describeWaitingCount,
  describeWaitingTime,
} from "../../lib/approvals";
import { personName } from "../../lib/people";
import { NO_RESIDENCY_CLAIM, describeResidencyClaim } from "../../lib/residency";
import { formatDate } from "../../lib/time";
import type { ApiError } from "../../api/types";

/**
 * Approvals is the council's queue of people waiting to be let into the town.
 *
 * It has a page of its own because it is work rather than a status: fifty
 * applicants is what a town launch or a registration flood produces, and the
 * Town Hall dashboard — which is meant to be read at a glance — was rendering
 * every one of them. The dashboard now keeps a preview and a way in here; this
 * is where somebody sits down and works through the list.
 *
 * Longest wait first, paged, and searchable by name, all of which are the
 * server's doing. The search asks the server rather than filtering the page in
 * hand, for the reason the directory does: filtering here would look right on
 * the first page and be wrong for every applicant past it.
 */
export default function Approvals() {
  const [search, setSearch] = useState("");
  // Debounced, so typing a name is one request rather than one per letter.
  const query = useDebouncedValue(search.trim());
  const { applicants, total, loading, hasMore, error, closed, loadMore, retry, removeApproved } =
    usePendingApprovals(query);

  const [approving, setApproving] = useState<string | null>(null);
  const [approveError, setApproveError] = useState<string | null>(null);

  // Read once, when the page loads. The waits are counted in hours and days, so
  // a clock re-read on every render would buy nothing and make the render
  // impure; the figures refresh when the page does.
  const [now] = useState(() => Date.now());

  const searching = query !== "";
  const countLine = describeWaitingCount(applicants.length, total, searching);
  // Only once the request has actually finished: an empty list mid-flight is a
  // list that has not arrived, not a town with nobody waiting.
  const showEmpty = applicants.length === 0 && !loading && !error && !closed;

  async function handleApprove(userId: string) {
    setApproving(userId);
    setApproveError(null);
    try {
      await approvalApi.approve(userId);
      // Dropped from this page rather than reloading the queue: see
      // usePendingApprovals. This may also have been the twentieth approval,
      // which ends bootstrap mode — the next load of this page will be refused
      // and say so, which is the honest way to find out.
      removeApproved(userId);
    } catch (err) {
      setApproveError((err as ApiError)?.error ?? "That approval did not go through.");
    } finally {
      setApproving(null);
    }
  }

  // No inner container: the route mounts this inside AppLayout's wide variant,
  // which centres it and supplies the gutter.
  return (
    <div className="py-5">
      <h1
        className="text-2xl font-bold"
        style={{ fontFamily: "var(--font-display)", color: "var(--color-text)" }}
      >
        Approvals
      </h1>
      <p className="mt-1 text-sm" style={{ color: "var(--color-text-secondary)" }}>
        Neighbours waiting to be rung in, longest wait first.
      </p>

      <div className="mt-5">
        <label
          htmlFor="approval-search"
          className="block text-sm font-medium"
          style={{ color: "var(--color-text-secondary)" }}
        >
          Find someone waiting
        </label>
        <input
          id="approval-search"
          type="search"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder="Start typing a name"
          autoComplete="off"
          className="mt-1 w-full rounded-[var(--radius-md)] border px-3 py-2 text-sm"
          style={{
            backgroundColor: "var(--color-surface)",
            borderColor: "var(--color-border-light)",
            color: "var(--color-text)",
          }}
        />
      </div>

      {countLine && (
        <p className="mt-3 text-xs" style={{ color: "var(--color-text-tertiary)" }}>
          {countLine}
        </p>
      )}

      {/*
        Not an error, so not an ErrorBanner. The town has left bootstrap mode —
        possibly on an approval made from this very page — and residents are
        admitted by vouch from here on. Saying "we could not load the queue"
        would send a council member hunting for an outage that is the town
        having grown up.
      */}
      {closed && (
        <div
          className="mt-4 rounded-[var(--radius-md)] p-4 text-sm"
          style={{
            backgroundColor: "var(--color-primary-subtle)",
            color: "var(--color-text-secondary)",
          }}
          role="status"
        >
          The town has left bootstrap mode, so the council no longer approves
          newcomers. Neighbours are rung in by vouch now.
        </div>
      )}

      {error && (
        <div className="mt-4">
          <ErrorBanner message={error} onRetry={retry} />
        </div>
      )}

      {approveError && (
        <div className="mt-4">
          <ErrorBanner message={approveError} />
        </div>
      )}

      {showEmpty && (
        <p className="mt-6 text-sm" style={{ color: "var(--color-text-tertiary)" }}>
          {searching ? APPROVALS_NO_MATCH : APPROVALS_EMPTY}
        </p>
      )}

      <ul className="mt-4 flex flex-col gap-2">
        {applicants.map((applicant) => {
          const name = personName(applicant.display_name, applicant.id);
          const joined = formatDate(applicant.joined_at);
          const waiting = describeWaitingTime(applicant.joined_at, now);
          // Joined with a separator only where both halves exist, so an
          // unparseable timestamp cannot leave a stray dot behind.
          const meta = [joined && `Joined ${joined}`, waiting].filter(Boolean).join(" · ");
          const said = describeResidencyClaim(applicant.residency_claim);

          return (
            <li
              key={applicant.id}
              className="flex items-center justify-between gap-4 p-3"
              style={{
                backgroundColor: "var(--color-surface)",
                boxShadow: "var(--shadow-sm)",
                borderRadius: "var(--radius-lg)",
              }}
            >
              <div className="min-w-0">
                {/* The fallback stays: most applicants arrive with a name now
                    that registration syncs the trait, but a council member
                    still has to be able to approve one who set none. */}
                <Link
                  to={`/profile/${applicant.id}`}
                  className="text-sm font-medium"
                  style={{ color: "var(--color-primary)" }}
                >
                  {name}
                </Link>
                {meta && (
                  <p className="text-xs" style={{ color: "var(--color-text-secondary)" }}>
                    {meta}
                  </p>
                )}
                {/*
                  Rendered as something this person said, never as an address
                  the town holds — see describeResidencyClaim. The queue is the
                  only place in the app it appears; it is deliberately absent
                  from profiles, from the directory and from the Town Hall
                  preview, and the server enforces that too. Somebody who gave
                  none gets a quiet line rather than a gap, so a council member
                  can tell "did not say" from "did not load".
                */}
                <p
                  className="mt-0.5 truncate text-xs italic"
                  style={{
                    color: said ? "var(--color-text-secondary)" : "var(--color-text-tertiary)",
                  }}
                  title={said ?? undefined}
                >
                  {said ?? NO_RESIDENCY_CLAIM}
                </p>
              </div>
              {/*
                Named for the person, not just the action: a screen reader
                moving through twenty-five rows would otherwise hear "Approve"
                twenty-five times with nothing to tell them apart.
              */}
              <button
                type="button"
                onClick={() => handleApprove(applicant.id)}
                disabled={approving === applicant.id}
                aria-label={`Approve ${name}`}
                className="flex-shrink-0 rounded-md px-3 py-1.5 text-sm font-medium disabled:cursor-not-allowed disabled:opacity-50"
                style={{
                  backgroundColor: "var(--color-success)",
                  color: "var(--color-text-inverse)",
                }}
              >
                {approving === applicant.id ? "Approving..." : "Approve"}
              </button>
            </li>
          );
        })}
      </ul>

      {loading && (
        <div className="flex justify-center py-6">
          <Spinner />
        </div>
      )}

      {/*
        A button rather than an infinite scroll, matching the directory. A queue
        is worked through from the top and a list that grows as you reach the
        bottom fights somebody scrolling back up to the name they just passed.
      */}
      {hasMore && !loading && applicants.length > 0 && (
        <div className="flex justify-center py-4">
          <button
            type="button"
            onClick={loadMore}
            className="rounded-[var(--radius-md)] px-4 py-2 text-sm font-medium"
            style={{
              backgroundColor: "var(--color-surface)",
              boxShadow: "var(--shadow-sm)",
              color: "var(--color-primary)",
            }}
          >
            Show more neighbours
          </button>
        </div>
      )}
    </div>
  );
}
