import { useRef, useState } from "react";
import { useAuth } from "../../context/AuthContext.tsx";
import { useModerationQueue } from "../../hooks/useModerationQueue.ts";
import { useIntersectionObserver } from "../../hooks/useIntersectionObserver.ts";
import ReportCard from "../../components/ReportCard.tsx";
import ActionDialog from "../../components/ActionDialog.tsx";
import ErrorBanner from "../../components/ErrorBanner.tsx";
import Spinner from "../../components/Spinner.tsx";
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
    <div className="py-5">
      <div className="mx-auto max-w-2xl p-4">
        <div className="mb-6">
          <h1
            className="text-2xl font-bold"
            style={{ fontFamily: "var(--font-display)", color: "var(--color-text)" }}
          >
            Moderation Queue
          </h1>
        </div>

        {error && (
          <div className="mb-4">
            <ErrorBanner message={error} onRetry={retry} />
          </div>
        )}

        {reports.length === 0 && !loading && !error && (
          <p style={{ color: "var(--color-text-tertiary)" }}>No pending reports.</p>
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
            <Spinner />
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
    </div>
  );
}
