import { useEffect, useState } from "react";
import { Link } from "react-router";
import { moderationApi } from "../api/client.ts";
import type { Report, Post, ApiError } from "../api/types.ts";
import Spinner from "./Spinner.tsx";
import { formatAbsoluteTime, formatRelativeTime } from "../lib/time.ts";
import { canRemovePost } from "../lib/moderation.ts";
import { personName } from "../lib/people.ts";

interface ReportCardProps {
  report: Report;
  currentUserId: string;
  onDismiss: (reportId: string) => void;
  onTakeAction: (report: Report, postAuthorId: string) => void;
  onRemovePost: (report: Report, postId: string) => void;
}

export default function ReportCard({
  report,
  currentUserId,
  onDismiss,
  onTakeAction,
  onRemovePost,
}: ReportCardProps) {
  const [post, setPost] = useState<Post | null>(null);
  const [postError, setPostError] = useState(false);
  const [dismissing, setDismissing] = useState(false);
  const [dismissError, setDismissError] = useState<string | null>(null);

  useEffect(() => {
    moderationApi
      .getPost(report.post_id)
      .then(setPost)
      .catch(() => setPostError(true));
  }, [report.post_id]);

  async function handleDismiss() {
    setDismissing(true);
    setDismissError(null);
    try {
      await moderationApi.updateReportStatus(report.id, "dismissed");
      onDismiss(report.id);
    } catch (err) {
      const apiErr = err as ApiError;
      // If 404, the report was already resolved by another moderator
      if (apiErr.status === 404) {
        onDismiss(report.id);
      } else {
        setDismissError(apiErr.error || "Failed to dismiss report");
      }
      setDismissing(false);
    }
  }

  const isOwnPost = post?.author_id === currentUserId;

  return (
    <article
      className="rounded-lg border p-4"
      style={{
        backgroundColor: "var(--color-surface)",
        boxShadow: "var(--shadow-sm)",
        borderRadius: "var(--radius-lg)",
        borderWidth: "1px",
        borderColor: "var(--color-border-light)",
      }}
    >
      {/* Report metadata */}
      <div className="mb-3 flex items-center justify-between">
        <span
          className="text-xs font-medium uppercase"
          style={{ color: "var(--color-danger)" }}
        >
          Report
        </span>
        <span
          className="text-xs"
          title={formatAbsoluteTime(report.created_at)}
          style={{ color: "var(--color-text-tertiary)" }}
        >
          {formatRelativeTime(report.created_at, { suffix: true })}
        </span>
      </div>

      <p className="mb-3 text-sm" style={{ color: "var(--color-text-secondary)" }}>
        <span className="font-medium">Reason:</span> {report.reason}
      </p>

      {/* Reported post content */}
      <div
        className="mb-3 rounded-md border p-3"
        style={{
          backgroundColor: "var(--color-surface-secondary)",
          borderColor: "var(--color-border-light)",
        }}
      >
        {postError ? (
          <p className="text-sm italic" style={{ color: "var(--color-text-tertiary)" }}>
            Post no longer available.
          </p>
        ) : post ? (
          <>
            <div className="mb-1 flex items-center justify-between">
              <span className="text-xs font-medium" style={{ color: "var(--color-text-secondary)" }}>
                {personName(post.author_display_name, post.author_id)}
              </span>
              <Link
                to={`/moderation/users/${post.author_id}`}
                className="text-xs hover:underline"
                style={{ color: "var(--color-primary)" }}
              >
                View history
              </Link>
            </div>
            {/* The same treatment the feed gives it: a moderator is judging
                what was actually said, so they should see it as the town did. */}
            <p className="post-body whitespace-pre-wrap break-words">{post.body}</p>
          </>
        ) : (
          <div className="flex justify-center py-2">
            <Spinner size="sm" />
          </div>
        )}
      </div>

      {/* Who filed it. A moderator weighing a report has to know that, and the
          queue is the only read that carries the reporter's name — the report
          echoed back to its own filer carries none. */}
      <p className="mb-3 text-xs" style={{ color: "var(--color-text-tertiary)" }}>
        Reporter: {personName(report.reporter_display_name, report.reporter_id)}
      </p>

      {/* Dismiss error */}
      {dismissError && (
        <p className="mb-2 text-sm" style={{ color: "var(--color-danger)" }}>
          {dismissError}
        </p>
      )}

      {/* Actions */}
      <div className="flex gap-2">
        <button
          onClick={handleDismiss}
          disabled={dismissing}
          className="btn btn-quiet rounded-md px-3 py-1.5 text-sm font-medium disabled:opacity-50"
        >
          {dismissing ? "Dismissing..." : "Dismiss"}
        </button>
        {/* The remedy for the post itself. Take Action below acts against the
            author instead; before this existed the offending post stayed up no
            matter what the moderator did. */}
        {canRemovePost(post) && (
          <button
            onClick={() => onRemovePost(report, report.post_id)}
            className="btn btn-danger-quiet rounded-md px-3 py-1.5 text-sm font-medium"
          >
            Remove Post
          </button>
        )}
        {post && !isOwnPost && (
          <button
            onClick={() => onTakeAction(report, post.author_id)}
            className="btn btn-danger rounded-md px-3 py-1.5 text-sm font-medium"
          >
            Take Action
          </button>
        )}
      </div>
    </article>
  );
}
