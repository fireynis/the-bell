import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Link } from "react-router";
import PostCard from "../components/PostCard.tsx";
import { NewPostsBanner } from "../components/NewPostsBanner.tsx";
import { Toast } from "../components/Toast.tsx";
import ErrorBanner from "../components/ErrorBanner.tsx";
import Spinner from "../components/Spinner.tsx";
import { useFeed } from "../hooks/useFeed.ts";
import { useLiveFeed } from "../hooks/useLiveFeed.ts";
import type { ReactionNotification } from "../hooks/useLiveFeed.ts";
import { useIntersectionObserver } from "../hooks/useIntersectionObserver.ts";
import { useSound } from "../hooks/useSound.ts";
import type { Post } from "../api/types.ts";

export default function Home() {
  const { posts, loading, hasMore, error, loadMore, retry } = useFeed();
  const sentinelRef = useRef<HTMLDivElement>(null);
  const [newPosts, setNewPosts] = useState<Post[]>([]);
  const [muted, setMuted] = useState(() => localStorage.getItem("bell-sound-muted") === "true");
  const [ringing, setRinging] = useState(false);
  const [toast, setToast] = useState<string | null>(null);
  const { playBell, playChime } = useSound();

  const toggleMute = () => {
    setMuted((prev) => {
      const next = !prev;
      localStorage.setItem("bell-sound-muted", String(next));
      return next;
    });
  };

  const handleReaction = useCallback((notification: ReactionNotification) => {
    const emoji: Record<string, string> = { bell: "\uD83D\uDD14", heart: "\u2764\uFE0F", celebrate: "\uD83C\uDF89" };
    const e = emoji[notification.reaction_type] || "\uD83D\uDC4D";
    if (notification.count === 1) {
      setToast(`Someone reacted ${e} to your post`);
    } else {
      setToast(`${notification.count} reactions on your post`);
    }
    if (!muted) {
      playChime();
    }
  }, [muted, playChime]);

  const allPosts = useMemo(() => [...newPosts, ...posts], [newPosts, posts]);
  const postIds = useMemo(() => new Set(allPosts.map((p) => p.id)), [allPosts]);

  const { pendingCount, pendingPosts, flush } = useLiveFeed(postIds, handleReaction);

  useIntersectionObserver(sentinelRef, loadMore, hasMore && !loading);

  const handleBannerClick = () => {
    setNewPosts((prev) => [...pendingPosts, ...prev]);
    flush();
  };

  // eslint-disable-next-line react-hooks/exhaustive-deps -- intentionally minimal deps: only trigger on count change
  useEffect(() => {
    if (pendingCount > 0 && !muted) {
      playBell();
    }
    if (pendingCount > 0) {
      setRinging(true);
      const timer = setTimeout(() => setRinging(false), 500);
      return () => clearTimeout(timer);
    }
  }, [pendingCount]);

  return (
    <div className="py-5">
      <div className="mb-5 flex items-center gap-3 lg:hidden">
        <Link
          to="/compose"
          className="flex flex-1 items-center gap-3 p-4"
          style={{
            backgroundColor: "var(--color-surface)",
            boxShadow: "var(--shadow-sm)",
            borderRadius: "var(--radius-lg)",
            color: "var(--color-text-tertiary)",
          }}
        >
          <div className="h-8 w-8 rounded-full" style={{ backgroundColor: "var(--color-primary-light)" }} />
          <span className="text-sm">What's happening in town?</span>
        </Link>
        <button
          onClick={toggleMute}
          className="p-2 rounded-lg hover:bg-gray-100 transition-colors"
          title={muted ? "Unmute notifications" : "Mute notifications"}
        >
          <span className={`text-xl ${ringing ? "animate-ring inline-block" : ""}`}>
            {muted ? "\uD83D\uDD15" : "\uD83D\uDD14"}
          </span>
        </button>
      </div>

      {error && (
        <div className="mb-4">
          <ErrorBanner message={error} onRetry={retry} />
        </div>
      )}

      <NewPostsBanner count={pendingCount} onClick={handleBannerClick} />

      {allPosts.length === 0 && !loading && !error && (
        <p className="text-center text-sm" style={{ color: "var(--color-text-tertiary)" }}>
          No posts yet. Be the first to ring the bell!
        </p>
      )}

      <div className="flex flex-col gap-3 stagger-children">
        {allPosts.map((post) => (
          <PostCard key={post.id} post={post} />
        ))}
      </div>

      {loading && (
        <div className="flex justify-center py-6">
          <Spinner />
        </div>
      )}

      <div ref={sentinelRef} className="h-1" />

      {toast && <Toast message={toast} onDismiss={() => setToast(null)} />}
    </div>
  );
}
