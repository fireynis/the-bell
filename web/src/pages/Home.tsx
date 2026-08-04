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
import { describeReactions, mergePendingPosts } from "../lib/liveFeed.ts";
import { mergeLivePosts } from "../lib/feed.ts";
import { useIntersectionObserver } from "../hooks/useIntersectionObserver.ts";
import { useSound } from "../hooks/useSound.ts";
import type { Post } from "../api/types.ts";

export default function Home() {
  const { posts, loading, hasMore, error, loadMore, retry } = useFeed();
  const sentinelRef = useRef<HTMLDivElement>(null);
  const [newPosts, setNewPosts] = useState<Post[]>([]);
  const [muted, setMuted] = useState(() => localStorage.getItem("bell-sound-muted") === "true");
  const [seenCount, setSeenCount] = useState(0);
  const [arrivals, setArrivals] = useState(0);
  const [toast, setToast] = useState<string | null>(null);
  const { playBell, playChime } = useSound();

  const toggleMute = () => {
    setMuted((prev) => {
      const next = !prev;
      localStorage.setItem("bell-sound-muted", String(next));
      return next;
    });
  };

  const handleReactions = useCallback((notifications: ReactionNotification[]) => {
    const message = describeReactions(notifications);
    if (!message) return;
    setToast(message);
    if (!muted) {
      playChime();
    }
  }, [muted, playChime]);

  const allPosts = useMemo(() => mergeLivePosts(newPosts, posts), [newPosts, posts]);
  const postIds = useMemo(() => new Set(allPosts.map((p) => p.id)), [allPosts]);
  // Only the paginated posts count as "already known" here: the live arrivals
  // being merged in below are themselves held in newPosts.
  const loadedIds = useMemo(() => new Set(posts.map((p) => p.id)), [posts]);

  const { pendingCount, pendingPosts, flush } = useLiveFeed(postIds, handleReactions);

  useIntersectionObserver(sentinelRef, loadMore, hasMore && !loading);

  const handleBannerClick = () => {
    setNewPosts((prev) => mergePendingPosts(pendingPosts, prev, loadedIds));
    flush();
  };

  // The bell announces an arrival, so it counts the moments the pending total
  // went up rather than reacting to the total itself. Anything else re-rings
  // for posts the reader has already been told about — most visibly when they
  // unmute, which is the last moment they want to be shouted at.
  if (pendingCount !== seenCount) {
    setSeenCount(pendingCount);
    if (pendingCount > seenCount) setArrivals((n) => n + 1);
  }

  // Read at ring time rather than depended on, so toggling the mute button is
  // not itself an arrival.
  const mutedRef = useRef(muted);
  useEffect(() => {
    mutedRef.current = muted;
  });

  useEffect(() => {
    if (arrivals > 0 && !mutedRef.current) {
      playBell();
    }
  }, [arrivals, playBell]);

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
          {/* Keyed on the arrival count so each arrival remounts the span and
              replays the one-shot CSS animation \u2014 no timer to reset. */}
          <span
            key={arrivals}
            className={`text-xl ${arrivals > 0 ? "animate-ring inline-block" : ""}`}
          >
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
