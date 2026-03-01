import { useCallback, useEffect, useRef, useState } from "react";
import { api } from "../api/client";
import type { FeedResponse, Post } from "../api/types";

const PAGE_SIZE = 20;

export function useFeed() {
  const [posts, setPosts] = useState<Post[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [hasMore, setHasMore] = useState(true);
  const cursorRef = useRef<string | undefined>(undefined);
  const fetchingRef = useRef(false);

  const fetchPage = useCallback(async (cursor?: string) => {
    if (fetchingRef.current) return;
    fetchingRef.current = true;
    setLoading(true);
    setError(null);

    try {
      const params = new URLSearchParams({ limit: String(PAGE_SIZE) });
      if (cursor) params.set("cursor", cursor);

      const data = await api.get<FeedResponse>(`/posts/?${params}`);
      const newPosts = data.posts ?? [];

      setPosts((prev) => (cursor ? [...prev, ...newPosts] : newPosts));
      cursorRef.current = data.next_cursor;
      setHasMore(!!data.next_cursor);
    } catch {
      setError("Failed to load posts. Please try again.");
    } finally {
      setLoading(false);
      fetchingRef.current = false;
    }
  }, []);

  useEffect(() => {
    fetchPage();
  }, [fetchPage]);

  const loadMore = useCallback(() => {
    if (!fetchingRef.current && hasMore && cursorRef.current) {
      fetchPage(cursorRef.current);
    }
  }, [fetchPage, hasMore]);

  const retry = useCallback(() => {
    setPosts([]);
    cursorRef.current = undefined;
    setHasMore(true);
    fetchPage();
  }, [fetchPage]);

  return { posts, loading, hasMore, error, loadMore, retry };
}
