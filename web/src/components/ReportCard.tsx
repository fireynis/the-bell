import { useEffect, useState } from "react";
import { Link } from "react-router";
import { moderationApi } from "../api/client.ts";
import type { Report, Post, ApiError } from "../api/types.ts";

function formatRelativeTime(dateStr: string): string {
  const date = new Date(dateStr);
  const now = Date.now();
  const diffMs = now - date.getTime();
  const diffSec = Math.floor(diffMs / 1000);

  if (diffSec < 60) return "just now";
  const diffMin = Math.floor(diffSec / 60);
  if (diffMin < 60) return `${diffMin}m ago`;
  const diffHr = Math.floor(diffMin / 60);
  if (diffHr < 24) return `${diffHr}h ago`;
  const diffDay = Math.floor(diffHr / 24);
  if (diffDay < 7) return `${diffDay}d ago`;
  return date.toLocaleDateString(undefined, { month: "short", day: "numeric" });
}

interface ReportCardProps {
  report: Report;
  currentUserId: string;
  onDismiss: (reportId: string) => void;
  onTakeAction: (report: Report, postAuthorId: string) => void;
}

export default function ReportCard({
  report,
  currentUserId,
  onDismiss,
  onTakeAction,
}: ReportCardProps) {
  const [post, setPost] = useState<Post | null>(null);
  const [postError, setPostError] = useState(false);
  const [dismissing, setDismissing] = useState(false);

  useEffect(() => {
    moderationApi
      .getPost(report.post_id)
      .then(setPost)
      .catch(() => setPostError(true));
  }, [report.post_id]);

  async function handleDismiss() {
    setDismissing(true);
    try {
      await moderationApi.updateReportStatus(report.id, "dismissed");
      onDismiss(report.id);
    } catch (err) {
      const apiErr = err as ApiError;
      // If 404, the report was already resolved by another moderator
      if (apiErr.status === 404) {
        onDismiss(report.id);
      }
      setDismissing(false);
    }
  }

  const isOwnPost = post?.author_id === currentUserId;

  return (
    <article className="rounded-lg border border-gray-200 bg-white p-4 shadow-sm">
      {/* Report metadata */}
      <div className="mb-3 flex items-center justify-between">
        <span className="text-xs font-medium text-red-600 uppercase">
          Report
        </span>
        <span
          className="text-xs text-gray-500"
          title={new Date(report.created_at).toLocaleString()}
        >
          {formatRelativeTime(report.created_at)}
        </span>
      </div>

      <p className="mb-3 text-sm text-gray-700">
        <span className="font-medium">Reason:</span> {report.reason}
      </p>

      {/* Reported post content */}
      <div className="mb-3 rounded-md border border-gray-100 bg-gray-50 p-3">
        {postError ? (
          <p className="text-sm text-gray-500 italic">
            Post no longer available.
          </p>
        ) : post ? (
          <>
            <div className="mb-1 flex items-center justify-between">
              <span className="text-xs font-medium text-gray-600">
                {post.author_id.slice(0, 8)}
              </span>
              <Link
                to={`/moderation/users/${post.author_id}`}
                className="text-xs text-indigo-600 hover:text-indigo-500"
              >
                View history
              </Link>
            </div>
            <p className="whitespace-pre-wrap break-words text-sm text-gray-800">
              {post.body}
            </p>
          </>
        ) : (
          <div className="flex justify-center py-2">
            <div className="h-4 w-4 animate-spin rounded-full border-2 border-gray-300 border-t-indigo-600" />
          </div>
        )}
      </div>

      {/* Reporter info */}
      <p className="mb-3 text-xs text-gray-500">
        Reporter: {report.reporter_id.slice(0, 8)}
      </p>

      {/* Actions */}
      <div className="flex gap-2">
        <button
          onClick={handleDismiss}
          disabled={dismissing}
          className="rounded-md bg-gray-100 px-3 py-1.5 text-sm font-medium text-gray-700 hover:bg-gray-200 disabled:opacity-50"
        >
          {dismissing ? "Dismissing..." : "Dismiss"}
        </button>
        {post && !isOwnPost && (
          <button
            onClick={() => onTakeAction(report, post.author_id)}
            className="rounded-md bg-red-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-red-500"
          >
            Take Action
          </button>
        )}
      </div>
    </article>
  );
}
