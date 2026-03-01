import { useRef } from "react";
import { useParams, Link } from "react-router";
import { useActionHistory } from "../../hooks/useActionHistory.ts";
import { useIntersectionObserver } from "../../hooks/useIntersectionObserver.ts";
import ActionHistoryCard from "../../components/ActionHistoryCard.tsx";

export default function UserHistory() {
  const { id } = useParams<{ id: string }>();
  const { entries, loading, hasMore, error, loadMore, retry } =
    useActionHistory(id!);
  const sentinelRef = useRef<HTMLDivElement>(null);

  useIntersectionObserver(sentinelRef, loadMore, hasMore && !loading);

  return (
    <div className="mx-auto max-w-2xl p-4">
      <div className="mb-6 flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold">User Action History</h1>
          <p className="text-sm text-gray-500">
            User: {id?.slice(0, 8)}...
          </p>
        </div>
        <Link
          to="/moderation"
          className="text-sm text-indigo-600 hover:text-indigo-500"
        >
          Back to Queue
        </Link>
      </div>

      {error && (
        <div className="mb-4 rounded-md bg-red-50 p-3 text-sm text-red-700">
          {error}
          <button onClick={retry} className="ml-2 font-medium underline">
            Retry
          </button>
        </div>
      )}

      {entries.length === 0 && !loading && !error && (
        <p className="text-gray-500">No moderation actions for this user.</p>
      )}

      <div className="flex flex-col gap-4">
        {entries.map((entry) => (
          <ActionHistoryCard key={entry.action.id} entry={entry} />
        ))}
      </div>

      {loading && (
        <div className="flex justify-center py-6">
          <div className="h-6 w-6 animate-spin rounded-full border-2 border-gray-300 border-t-indigo-600" />
        </div>
      )}

      <div ref={sentinelRef} className="h-1" />
    </div>
  );
}
