import { useRef, useState } from "react";
import { Link } from "react-router";
import { useAuth } from "../../context/AuthContext.tsx";
import { useModerationQueue } from "../../hooks/useModerationQueue.ts";
import { useIntersectionObserver } from "../../hooks/useIntersectionObserver.ts";
import ReportCard from "../../components/ReportCard.tsx";
import ActionDialog from "../../components/ActionDialog.tsx";
import type { Report } from "../../api/types.ts";

interface ActionTarget {
  report: Report;
  targetUserId: string;
}

export default function Queue() {
  const { user } = useAuth();
  const { reports, loading, hasMore, error, loadMore, removeReport, retry } =
    useModerationQueue();
  const sentinelRef = useRef<HTMLDivElement>(null);
  const [actionTarget, setActionTarget] = useState<ActionTarget | null>(null);

  useIntersectionObserver(sentinelRef, loadMore, hasMore && !loading);

  function handleTakeAction(report: Report, postAuthorId: string) {
    setActionTarget({ report, targetUserId: postAuthorId });
  }

  function handleActionTaken() {
    if (actionTarget) {
      removeReport(actionTarget.report.id);
    }
    setActionTarget(null);
  }

  return (
    <div className="mx-auto max-w-2xl p-4">
      <div className="mb-6 flex items-center justify-between">
        <h1 className="text-2xl font-bold">Moderation Queue</h1>
        <Link
          to="/"
          className="text-sm text-indigo-600 hover:text-indigo-500"
        >
          Back to Feed
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

      {reports.length === 0 && !loading && !error && (
        <p className="text-gray-500">No pending reports.</p>
      )}

      <div className="flex flex-col gap-4">
        {reports.map((report) => (
          <ReportCard
            key={report.id}
            report={report}
            currentUserId={user?.id ?? ""}
            onDismiss={removeReport}
            onTakeAction={handleTakeAction}
          />
        ))}
      </div>

      {loading && (
        <div className="flex justify-center py-6">
          <div className="h-6 w-6 animate-spin rounded-full border-2 border-gray-300 border-t-indigo-600" />
        </div>
      )}

      <div ref={sentinelRef} className="h-1" />

      {actionTarget && (
        <ActionDialog
          targetUserId={actionTarget.targetUserId}
          onClose={() => setActionTarget(null)}
          onActionTaken={handleActionTaken}
        />
      )}
    </div>
  );
}
