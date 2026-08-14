import { useState } from "react";
import { Link } from "react-router";
import Avatar from "../components/Avatar";
import ErrorBanner from "../components/ErrorBanner";
import Spinner from "../components/Spinner";
import { useAuth } from "../context/AuthContext";
import { useDebouncedValue } from "../hooks/useDebouncedValue";
import { useNeighbors } from "../hooks/useNeighbors";
import { NEIGHBORS_INVITE, describeNeighborCount, roleLabel } from "../lib/directory";
import { awaitingWelcome } from "../lib/gating";
import { personName } from "../lib/people";
import { formatMonthYear } from "../lib/time";

/**
 * Neighbors lists everybody on the town's roll.
 *
 * It is a discovery page and nothing more: vouching happens on a profile, where
 * there is room to say who somebody is before you put your name behind theirs.
 * What was missing was any way to find that profile at all — a member who knew
 * a neighbour by name had no way to reach them without a link from somewhere
 * else, which is a strange gap in a town whose entire mechanic is knowing each
 * other.
 */
export default function Neighbors() {
  const { user } = useAuth();
  const [search, setSearch] = useState("");
  // Debounced, so typing a name is one request rather than one per letter.
  const query = useDebouncedValue(search.trim());
  const { neighbors, total, loading, hasMore, error, loadMore, retry } = useNeighbors(query);

  const searching = query !== "";
  const countLine = describeNeighborCount(neighbors.length, total, searching);
  // Only once the request has actually finished: an empty list mid-flight is a
  // list that has not arrived, not a town with nobody in it.
  const showEmpty = neighbors.length === 0 && !loading && !error;

  return (
    <div className="py-5">
      <div className="mx-auto max-w-2xl px-4">
        <h1
          className="text-2xl font-bold"
          style={{ fontFamily: "var(--font-display)", color: "var(--color-text)" }}
        >
          Neighbours
        </h1>
        <p className="mt-1 text-sm" style={{ color: "var(--color-text-secondary)" }}>
          Everyone on the town's roll, newest arrivals first.
        </p>

        {/*
          For somebody the town has not rung in yet. The directory is the one
          place they can act on their own situation — every name here is a
          person who could vouch for them — so the invitation belongs at the top
          of it rather than buried under the list.
        */}
        {awaitingWelcome(user) && (
          <section
            className="mt-4 rounded-[var(--radius-lg)] p-4"
            style={{
              backgroundColor: "var(--color-primary-subtle)",
              border: "1px solid var(--color-primary-light)",
            }}
            role="status"
            aria-label={NEIGHBORS_INVITE.title}
            data-testid="neighbors-invite"
          >
            <h2
              className="text-base font-semibold"
              style={{ fontFamily: "var(--font-display)", color: "var(--color-text)" }}
            >
              {NEIGHBORS_INVITE.title}
            </h2>
            <p
              className="mt-2 text-sm leading-relaxed"
              style={{ color: "var(--color-text-secondary)" }}
            >
              {NEIGHBORS_INVITE.body}
            </p>
          </section>
        )}

        <div className="mt-5">
          <label
            htmlFor="neighbor-search"
            className="block text-sm font-medium"
            style={{ color: "var(--color-text-secondary)" }}
          >
            Find a neighbour
          </label>
          <input
            id="neighbor-search"
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

        {error && (
          <div className="mt-4">
            <ErrorBanner message={error} onRetry={retry} />
          </div>
        )}

        {showEmpty && (
          <p className="mt-6 text-sm" style={{ color: "var(--color-text-tertiary)" }}>
            {searching ? "Nobody by that name yet." : "Nobody is on the roll yet."}
          </p>
        )}

        <ul className="mt-4 flex flex-col gap-2">
          {neighbors.map((neighbor) => {
            const name = personName(neighbor.display_name, neighbor.id);
            const joined = formatMonthYear(neighbor.joined_at);
            // Joined with a separator only where both halves exist: an unknown
            // role or an unparseable date must not leave a stray dot behind.
            const meta = [roleLabel(neighbor.role), joined && `Joined ${joined}`]
              .filter(Boolean)
              .join(" · ");

            return (
              <li key={neighbor.id}>
                <Link
                  to={`/profile/${neighbor.id}`}
                  className="flex items-center gap-3 p-3"
                  style={{
                    backgroundColor: "var(--color-surface)",
                    boxShadow: "var(--shadow-sm)",
                    borderRadius: "var(--radius-lg)",
                  }}
                >
                  {/*
                    The directory row carries no avatar url — the listing does
                    not send one — so this is the initial, and it is hidden from
                    assistive tech: read aloud it would prepend a stray letter
                    to a link whose name is already the person's name.
                  */}
                  <span aria-hidden="true" className="flex-shrink-0">
                    <Avatar url="" name={name} size="md" />
                  </span>
                  <span className="min-w-0 flex-1">
                    <span
                      className="block truncate text-sm font-medium"
                      style={{ color: "var(--color-text)" }}
                    >
                      {name}
                    </span>
                    {meta && (
                      <span
                        className="block truncate text-xs"
                        style={{ color: "var(--color-text-tertiary)" }}
                      >
                        {meta}
                      </span>
                    )}
                  </span>
                </Link>
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
          A button rather than an infinite scroll, unlike the feed. A directory
          is somewhere you search and stop, and a list that grows as you reach
          the bottom of it fights the person scanning back up for a name they
          have already passed.
        */}
        {hasMore && !loading && neighbors.length > 0 && (
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
    </div>
  );
}
