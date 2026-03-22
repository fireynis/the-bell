import { useRef } from "react";
import { Link } from "react-router";
import PostCard from "../components/PostCard.tsx";
import ErrorBanner from "../components/ErrorBanner.tsx";
import Spinner from "../components/Spinner.tsx";
import { useFeed } from "../hooks/useFeed.ts";
import { useIntersectionObserver } from "../hooks/useIntersectionObserver.ts";

export default function Home() {
  const { posts, loading, hasMore, error, loadMore, retry } = useFeed();
  const sentinelRef = useRef<HTMLDivElement>(null);

  useIntersectionObserver(sentinelRef, loadMore, hasMore && !loading);

  return (
    <div className="py-5">
      <Link
        to="/compose"
        className="mb-5 flex items-center gap-3 p-4 lg:hidden"
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

      {error && (
        <div className="mb-4">
          <ErrorBanner message={error} onRetry={retry} />
        </div>
      )}

      {posts.length === 0 && !loading && !error && (
        <p className="text-center text-sm" style={{ color: "var(--color-text-tertiary)" }}>
          No posts yet. Be the first to ring the bell!
        </p>
      )}

      <div className="flex flex-col gap-3 stagger-children">
        {posts.map((post) => (
          <PostCard key={post.id} post={post} />
        ))}
      </div>

      {loading && (
        <div className="flex justify-center py-6">
          <Spinner />
        </div>
      )}

      <div ref={sentinelRef} className="h-1" />
    </div>
  );
}
