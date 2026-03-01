import type { Post } from "../api/types";
import ReactionButton from "./ReactionButton";

const REACTION_TYPES = ["bell", "heart", "celebrate"];

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

interface PostCardProps {
  post: Post;
}

export default function PostCard({ post }: PostCardProps) {
  const authorShort = post.author_id.slice(0, 8);

  return (
    <article className="rounded-lg bg-white p-4 shadow">
      <div className="mb-2 flex items-center justify-between">
        <span className="text-sm font-medium text-gray-900">{authorShort}</span>
        <span className="text-xs text-gray-500" title={new Date(post.created_at).toLocaleString()}>
          {formatRelativeTime(post.created_at)}
          {post.edited_at && " (edited)"}
        </span>
      </div>

      <p className="mb-3 whitespace-pre-wrap break-words text-gray-800">{post.body}</p>

      {post.image_path && (
        <img
          src={post.image_path}
          alt=""
          className="mb-3 max-h-96 rounded-md object-cover"
        />
      )}

      <div className="flex gap-2">
        {REACTION_TYPES.map((type) => (
          <ReactionButton
            key={type}
            postId={post.id}
            type={type}
            count={0}
            active={false}
          />
        ))}
      </div>
    </article>
  );
}
