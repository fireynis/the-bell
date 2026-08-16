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

  // Coalesced once here rather than at each use: the field is absent on posts
  // served from a feed-cache entry marshalled before it existed, and `undefined`
  // reaching an alt attribute would drop the attribute entirely — which makes a
  // screen reader read the filename instead of staying silent.
  const altText = post.alt_text ?? "";

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

  // The card carries no hover treatment: it is not a link, and lifting it on
  // hover promised a click that has never done anything.
  return (
    <article className="post-card animate-fade-in-up p-5">
      <div className="mb-3 flex items-center gap-2">
        <Avatar url={post.author_avatar_url || ""} name={authorName} size="sm" />
        <Link to={`/profile/${post.author_id}`} className="author-link text-sm font-semibold">
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

      {/*
        The one thing on the page that is not the app talking. It is set in the
        display serif at reading size so a post carries more weight than the
        chrome around it — which used to be the same size and nearly the same
        colour, leaving the town's own voice indistinguishable from a timestamp.
      */}
      <p className="post-body mb-4 whitespace-pre-wrap break-words">{post.body}</p>

      {post.image_path && (
        <>
          {/* A button, not a bare img with onClick: opening the full-size image
              was reachable by mouse only. */}
          <button
            type="button"
            onClick={() => setLightbox(true)}
            // The description belongs on the button, because the button is what
            // a screen reader announces — the img inside it is decorative from
            // the accessibility tree's point of view, and an alt here as well
            // would have the description read out twice. Without a description
            // this falls back to naming the action alone, which is what it has
            // always said.
            aria-label={
              altText
                ? `View ${authorName}'s image full size: ${altText}`
                : `View ${authorName}'s image full size`
            }
            className="mb-4 mt-3 block w-full overflow-hidden rounded-lg"
          >
            <img
              src={post.image_path}
              alt=""
              loading="lazy"
              className="max-h-96 w-full object-cover transition-opacity hover:opacity-90"
            />
          </button>
          {lightbox && (
            <ImageLightbox
              src={post.image_path}
              // Here the image is the content rather than the label on a
              // control, so the description goes on the img itself. An
              // undescribed image passes "" and is skipped, which is correct:
              // announcing a filename would be worse than announcing nothing.
              alt={altText}
              onClose={() => setLightbox(false)}
            />
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
