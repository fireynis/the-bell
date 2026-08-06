import { useRef, useState } from "react";
import { useAuth } from "../../context/AuthContext.tsx";
import { useModerationQueue } from "../../hooks/useModerationQueue.ts";
import { useIntersectionObserver } from "../../hooks/useIntersectionObserver.ts";
import ReportCard from "../../components/ReportCard.tsx";
import ActionDialog from "../../components/ActionDialog.tsx";
import RemovePostDialog from "../../components/RemovePostDialog.tsx";
import ErrorBanner from "../../components/ErrorBanner.tsx";
import Spinner from "../../components/Spinner.tsx";
import type { Report } from "../../api/types.ts";

interface ActionTarget {
  report: Report;
  targetUserId: string;
}

interface RemovalTarget {
  report: Report;
  postId: string;
}

export default function Queue() {
  const { user } = useAuth();
  const { reports, loading, hasMore, error, loadMore, removeReport, resolveReport, retry } =
    useModerationQueue();
  const sentinelRef = useRef<HTMLDivElement>(null);
  const [actionTarget, setActionTarget] = useState<ActionTarget | null>(null);
  const [removalTarget, setRemovalTarget] = useState<RemovalTarget | null>(null);
  const [resolutionError, setResolutionError] = useState<string | null>(null);

  useIntersectionObserver(sentinelRef, loadMore, hasMore && !loading);

  function handleTakeAction(report: Report, postAuthorId: string) {
    setActionTarget({ report, targetUserId: postAuthorId });
  }

  // Acting on a report and resolving the report are separate writes: neither
  // the moderation-action endpoint nor the removal endpoint touches report
  // status, because a moderator may act on a post that was never reported. So
  // marking it reviewed is this page's job, and it is the half that used to be
  // missing entirely.
  async function handleActionTaken() {
    const target = actionTarget;
    setActionTarget(null);
    if (target) {
      setResolutionError((await resolveReport(target.report.id)).error ?? null);
    }
  }

  function handleRemovePost(report: Report, postId: string) {
    setRemovalTarget({ report, postId });
  }

  async function handlePostRemoved() {
    const target = removalTarget;
    setRemovalTarget(null);
    if (target) {
      setResolutionError((await resolveReport(target.report.id)).error ?? null);
    }
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

        {/* The action succeeded but the report is still pending, so the card is
            still below. Saying so beats letting the two disagree in silence. */}
        {resolutionError && (
          <div className="mb-4">
            <ErrorBanner message={resolutionError} />
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
              onRemovePost={handleRemovePost}
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

        {removalTarget && (
          <RemovePostDialog
            postId={removalTarget.postId}
            onClose={() => setRemovalTarget(null)}
            onRemoved={handlePostRemoved}
          />
        )}
      </div>
    </div>
  );
}
