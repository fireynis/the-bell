import { useCallback, useEffect, useRef, useState } from "react";
import { api } from "../api/client";
import type { FeedResponse } from "../api/types";
import { applyFeedPage, initialFeedState, type FeedState } from "../lib/feed";

const PAGE_SIZE = 20;

export function useFeed() {
  const [feed, setFeed] = useState<FeedState>(initialFeedState);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
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
      setFeed((prev) => applyFeedPage(prev, data, !!cursor));
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
    if (!fetchingRef.current && feed.hasMore && feed.cursor) {
      fetchPage(feed.cursor);
    }
  }, [fetchPage, feed.hasMore, feed.cursor]);

  const retry = useCallback(() => {
    setFeed(initialFeedState());
    fetchPage();
  }, [fetchPage]);

  return {
    posts: feed.posts,
    loading,
    hasMore: feed.hasMore,
    error,
    loadMore,
    retry,
  };
}
