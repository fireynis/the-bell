import { useRef } from "react";
import { Link } from "react-router";
import { useAuth } from "../context/AuthContext.tsx";
import PostCard from "../components/PostCard.tsx";
import { useFeed } from "../hooks/useFeed.ts";
import { useIntersectionObserver } from "../hooks/useIntersectionObserver.ts";

export default function Home() {
  const { session, logout } = useAuth();
  const { posts, loading, hasMore, error, loadMore, retry } = useFeed();
  const sentinelRef = useRef<HTMLDivElement>(null);

  useIntersectionObserver(sentinelRef, loadMore, hasMore && !loading);

  return (
    <div className="mx-auto max-w-2xl p-4">
      <div className="mb-6 flex items-center justify-between">
        <h1 className="text-2xl font-bold">Feed</h1>
        <div className="flex items-center gap-3">
          {session && (
            <span className="text-sm text-gray-600">
              {session.identity.traits.name ?? session.identity.traits.email}
            </span>
          )}
          <Link
            to="/auth/settings"
            className="text-sm text-indigo-600 hover:text-indigo-500"
          >
            Settings
          </Link>
          <button
            onClick={logout}
            className="rounded-md bg-gray-100 px-3 py-1 text-sm text-gray-700 hover:bg-gray-200"
          >
            Sign out
          </button>
        </div>
      </div>

      {error && (
        <div className="mb-4 rounded-md bg-red-50 p-3 text-sm text-red-700">
          {error}
          <button onClick={retry} className="ml-2 font-medium underline">
            Retry
          </button>
        </div>
      )}

      {posts.length === 0 && !loading && !error && (
        <p className="text-gray-500">No posts yet.</p>
      )}

      <div className="flex flex-col gap-4">
        {posts.map((post) => (
          <PostCard key={post.id} post={post} />
        ))}
      </div>

      {loading && (
        <div className="flex justify-center py-6">
          <div className="h-6 w-6 animate-spin rounded-full border-2 border-gray-300 border-t-indigo-600" />
        </div>
      )}

      <div ref={sentinelRef} className="h-1" />
    </div>
  );
}
