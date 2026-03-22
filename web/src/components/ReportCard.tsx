import { useEffect, useState } from "react";
import { Link } from "react-router";
import { moderationApi } from "../api/client.ts";
import type { Report, Post, ApiError } from "../api/types.ts";
import Spinner from "./Spinner.tsx";

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
          title={new Date(report.created_at).toLocaleString()}
          style={{ color: "var(--color-text-tertiary)" }}
        >
          {formatRelativeTime(report.created_at)}
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
                {post.author_id.slice(0, 8)}
              </span>
              <Link
                to={`/moderation/users/${post.author_id}`}
                className="text-xs hover:underline"
                style={{ color: "var(--color-primary)" }}
              >
                View history
              </Link>
            </div>
            <p
              className="whitespace-pre-wrap break-words text-sm"
              style={{ color: "var(--color-text)" }}
            >
              {post.body}
            </p>
          </>
        ) : (
          <div className="flex justify-center py-2">
            <Spinner size="sm" />
          </div>
        )}
      </div>

      {/* Reporter info */}
      <p className="mb-3 text-xs" style={{ color: "var(--color-text-tertiary)" }}>
        Reporter: {report.reporter_id.slice(0, 8)}
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
          className="rounded-md px-3 py-1.5 text-sm font-medium disabled:opacity-50"
          style={{
            backgroundColor: "var(--color-surface-tertiary)",
            color: "var(--color-text-secondary)",
          }}
          onMouseEnter={(e) => {
            (e.currentTarget as HTMLButtonElement).style.filter = "brightness(0.95)";
          }}
          onMouseLeave={(e) => {
            (e.currentTarget as HTMLButtonElement).style.filter = "";
          }}
        >
          {dismissing ? "Dismissing..." : "Dismiss"}
        </button>
        {post && !isOwnPost && (
          <button
            onClick={() => onTakeAction(report, post.author_id)}
            className="rounded-md px-3 py-1.5 text-sm font-medium"
            style={{
              backgroundColor: "var(--color-danger)",
              color: "var(--color-text-inverse)",
            }}
            onMouseEnter={(e) => {
              (e.currentTarget as HTMLButtonElement).style.filter = "brightness(1.1)";
            }}
            onMouseLeave={(e) => {
              (e.currentTarget as HTMLButtonElement).style.filter = "";
            }}
          >
            Take Action
          </button>
        )}
      </div>
    </article>
  );
}
