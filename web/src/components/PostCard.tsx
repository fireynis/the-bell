import { Link } from "react-router";
import type { Post } from "../api/types";
import Avatar from "./Avatar";
import ReactionButton from "./ReactionButton";

const REACTION_TYPES = ["bell", "heart", "celebrate"];

function formatRelativeTime(dateStr: string): string {
  const date = new Date(dateStr);
  const now = Date.now();
  const diffMs = now - date.getTime();
  const diffSec = Math.floor(diffMs / 1000);
  if (diffSec < 60) return "just now";
  const diffMin = Math.floor(diffSec / 60);
  if (diffMin < 60) return `${diffMin}m`;
  const diffHr = Math.floor(diffMin / 60);
  if (diffHr < 24) return `${diffHr}h`;
  const diffDay = Math.floor(diffHr / 24);
  if (diffDay < 7) return `${diffDay}d`;
  return date.toLocaleDateString(undefined, { month: "short", day: "numeric" });
}

interface PostCardProps {
  post: Post;
}

export default function PostCard({ post }: PostCardProps) {
  const authorName = post.author_display_name || post.author_id.slice(0, 8);

  return (
    <article
      className="animate-fade-in-up p-5 transition-shadow"
      style={{
        backgroundColor: "var(--color-surface)",
        boxShadow: "var(--shadow-sm)",
        borderRadius: "var(--radius-lg)",
      }}
      onMouseEnter={(e) => (e.currentTarget.style.boxShadow = "var(--shadow-md)")}
      onMouseLeave={(e) => (e.currentTarget.style.boxShadow = "var(--shadow-sm)")}
    >
      <div className="mb-3 flex items-center gap-2">
        <Avatar url={post.author_avatar_url || ""} name={authorName} size="sm" />
        <Link
          to={`/profile/${post.author_id}`}
          className="text-sm font-semibold transition-colors"
          style={{ color: "var(--color-text)" }}
          onMouseEnter={(e) => (e.currentTarget.style.color = "var(--color-primary)")}
          onMouseLeave={(e) => (e.currentTarget.style.color = "var(--color-text)")}
        >
          {authorName}
        </Link>
        <span style={{ color: "var(--color-text-tertiary)" }}>&middot;</span>
        <span
          className="text-xs"
          style={{ color: "var(--color-text-tertiary)" }}
          title={new Date(post.created_at).toLocaleString()}
        >
          {formatRelativeTime(post.created_at)}
          {post.edited_at && " (edited)"}
        </span>
      </div>

      <p
        className="mb-4 whitespace-pre-wrap break-words leading-relaxed"
        style={{ color: "var(--color-text)", fontSize: "0.9375rem" }}
      >
        {post.body}
      </p>

      {post.image_path && (
        <img
          src={post.image_path}
          alt=""
          className="mb-4 max-h-96 w-full object-cover"
          style={{ borderRadius: "var(--radius-md)" }}
        />
      )}

      <div className="flex gap-1.5">
        {REACTION_TYPES.map((type) => (
          <ReactionButton key={type} postId={post.id} type={type} count={0} active={false} />
        ))}
      </div>
    </article>
  );
}
