import { useState } from "react";
import { Link } from "react-router";
import { postApi } from "../api/client";
import type { ApiError, Post } from "../api/types";
import { useAuth } from "../context/AuthContext";
import Avatar from "./Avatar";
import ConfirmDialog from "./ConfirmDialog";
import EditPostDialog from "./EditPostDialog";
import { ImageLightbox } from "./ImageLightbox";
import PostMenu, { type PostMenuItem } from "./PostMenu";
import ReactionButton from "./ReactionButton";
import ReportDialog from "./ReportDialog";
import { formatAbsoluteTime, formatRelativeTime } from "../lib/time";
import { canEditPost, canDeletePost, postMutationErrorMessage } from "../lib/post";
import { canReportPost } from "../lib/report";
import { REACTION_TYPES } from "../lib/reactions";

/** Which dialog, if any, the card currently has open. */
type OpenDialog = "edit" | "delete" | "report" | null;

interface PostCardProps {
  post: Post;
  /**
   * Called with the server's copy of the post after its author edits it. A card
   * without this handler still saves the edit; it just shows the old body until
   * the list is loaded again.
   */
  onUpdated?: (post: Post) => void;
  /** Called after the author deletes their post, so the list can drop the card. */
  onRemoved?: (postId: string) => void;
}

export default function PostCard({ post, onUpdated, onRemoved }: PostCardProps) {
  const { user } = useAuth();
  const [lightbox, setLightbox] = useState(false);
  const [dialog, setDialog] = useState<OpenDialog>(null);
  const [reported, setReported] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [deleteError, setDeleteError] = useState<string | null>(null);
  const [selfRemoved, setSelfRemoved] = useState(false);

  // Re-read when the menu opens rather than on a timer: the only rule that
  // changes with the clock is the edit window, and nobody needs the Edit item to
  // disappear from a menu they are not looking at.
  const [now, setNow] = useState(() => Date.now());

  const authorName = post.author_display_name || post.author_id.slice(0, 8);
  const viewerId = user?.id ?? null;

  const items: PostMenuItem[] = [];
  if (canEditPost(post, viewerId, now)) {
    items.push({ label: "Edit post", onSelect: () => setDialog("edit") });
  }
  if (canDeletePost(post, viewerId)) {
    items.push({ label: "Delete post", danger: true, onSelect: () => setDialog("delete") });
  }
  if (canReportPost(post, user)) {
    // Kept in place once sent rather than dropped, so the menu answers "did that
    // go through?" — and a second report on the same post is a guaranteed 400.
    items.push(
      reported
        ? { label: "Reported", disabled: true, onSelect: () => {} }
        : { label: "Report post", onSelect: () => setDialog("report") },
    );
  }

  async function handleDelete() {
    if (deleting) return;
    setDeleting(true);
    setDeleteError(null);

    try {
      await postApi.remove(post.id);
      setDialog(null);
      // Hidden here as well as reported upward: a confirmed delete that leaves
      // the card sitting there reads as a failure, and not every list that
      // renders a post card tracks its own copy of the feed.
      setSelfRemoved(true);
      onRemoved?.(post.id);
    } catch (err) {
      setDeleteError(postMutationErrorMessage(err as ApiError, "delete"));
      setDeleting(false);
    }
  }

  if (selfRemoved) return null;

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
          title={formatAbsoluteTime(post.created_at)}
        >
          {formatRelativeTime(post.created_at)}
          {post.edited_at && " (edited)"}
        </span>

        <div className="ml-auto">
          <PostMenu
            items={items}
            label={`Actions for ${authorName}'s post`}
            onOpen={() => setNow(Date.now())}
          />
        </div>
      </div>

      <p
        className="mb-4 whitespace-pre-wrap break-words leading-relaxed"
        style={{ color: "var(--color-text)", fontSize: "0.9375rem" }}
      >
        {post.body}
      </p>

      {post.image_path && (
        <>
          <img
            src={post.image_path}
            alt=""
            loading="lazy"
            onClick={() => setLightbox(true)}
            className="mb-4 mt-3 rounded-lg max-h-96 w-full object-cover cursor-pointer hover:opacity-90 transition-opacity"
          />
          {lightbox && (
            <ImageLightbox src={post.image_path} onClose={() => setLightbox(false)} />
          )}
        </>
      )}

      <div className="flex gap-1.5">
        {REACTION_TYPES.map((type) => (
          <ReactionButton
            key={type}
            postId={post.id}
            type={type}
            count={post.reaction_counts?.[type] ?? 0}
            active={post.user_reactions?.includes(type) ?? false}
          />
        ))}
      </div>

      {dialog === "edit" && (
        <EditPostDialog
          post={post}
          onClose={() => setDialog(null)}
          onSaved={(updated) => onUpdated?.(updated)}
        />
      )}

      {dialog === "delete" && (
        <ConfirmDialog
          title="Delete your post?"
          body="It stops being visible to the town. Moderators can still see it, and any reports already filed against it stay in their queue."
          confirmLabel="Delete post"
          danger
          busy={deleting}
          error={deleteError}
          onConfirm={handleDelete}
          onCancel={() => {
            setDialog(null);
            setDeleteError(null);
          }}
        />
      )}

      {dialog === "report" && (
        <ReportDialog
          postId={post.id}
          authorName={authorName}
          onClose={() => setDialog(null)}
          onReported={() => setReported(true)}
        />
      )}
    </article>
  );
}
