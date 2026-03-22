import { useRef } from "react";
import { useParams } from "react-router";
import { useActionHistory } from "../../hooks/useActionHistory.ts";
import { useIntersectionObserver } from "../../hooks/useIntersectionObserver.ts";
import ActionHistoryCard from "../../components/ActionHistoryCard.tsx";
import ErrorBanner from "../../components/ErrorBanner.tsx";
import Spinner from "../../components/Spinner.tsx";

export default function UserHistory() {
  const { id } = useParams<{ id: string }>();
  const { entries, loading, hasMore, error, loadMore, retry } =
    useActionHistory(id!);
  const sentinelRef = useRef<HTMLDivElement>(null);

  useIntersectionObserver(sentinelRef, loadMore, hasMore && !loading);

  return (
    <div className="py-5">
      <div className="mx-auto max-w-2xl p-4">
        <div className="mb-6">
          <h1
            className="text-2xl font-bold"
            style={{ fontFamily: "var(--font-display)", color: "var(--color-text)" }}
          >
            User Action History
          </h1>
          <p className="text-sm" style={{ color: "var(--color-text-tertiary)" }}>
            User: {id?.slice(0, 8)}...
          </p>
        </div>

        {error && (
          <div className="mb-4">
            <ErrorBanner message={error} onRetry={retry} />
          </div>
        )}

        {entries.length === 0 && !loading && !error && (
          <p style={{ color: "var(--color-text-tertiary)" }}>
            No moderation actions for this user.
          </p>
        )}

        <div className="flex flex-col gap-4">
          {entries.map((entry) => (
            <ActionHistoryCard key={entry.action.id} entry={entry} />
          ))}
        </div>

        {loading && (
          <div className="flex justify-center py-6">
            <Spinner />
          </div>
        )}

        <div ref={sentinelRef} className="h-1" />
      </div>
    </div>
  );
}
