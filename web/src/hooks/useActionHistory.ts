import { useCallback, useEffect, useRef, useState } from "react";
import { moderationApi } from "../api/client";
import type { ActionHistoryEntry } from "../api/types";

const PAGE_SIZE = 20;

export function useActionHistory(userId: string) {
  const [entries, setEntries] = useState<ActionHistoryEntry[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [hasMore, setHasMore] = useState(true);
  const offsetRef = useRef(0);
  const fetchingRef = useRef(false);

  const fetchPage = useCallback(
    async (offset: number, append: boolean) => {
      if (fetchingRef.current) return;
      fetchingRef.current = true;
      setLoading(true);
      setError(null);

      try {
        const data = await moderationApi.getActionHistory(
          userId,
          PAGE_SIZE,
          offset,
        );
        const newEntries = data.actions ?? [];

        setEntries((prev) =>
          append ? [...prev, ...newEntries] : newEntries,
        );
        offsetRef.current = offset + newEntries.length;
        setHasMore(newEntries.length >= PAGE_SIZE);
      } catch {
        setError("Failed to load action history.");
      } finally {
        setLoading(false);
        fetchingRef.current = false;
      }
    },
    [userId],
  );

  useEffect(() => {
    setEntries([]);
    offsetRef.current = 0;
    setHasMore(true);
    fetchPage(0, false);
  }, [fetchPage]);

  const loadMore = useCallback(() => {
    if (!fetchingRef.current && hasMore) {
      fetchPage(offsetRef.current, true);
    }
  }, [fetchPage, hasMore]);

  const retry = useCallback(() => {
    setEntries([]);
    offsetRef.current = 0;
    setHasMore(true);
    fetchPage(0, false);
  }, [fetchPage]);

  return { entries, loading, hasMore, error, loadMore, retry };
}
