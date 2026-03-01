import { useCallback, useEffect, useRef, useState } from "react";
import { moderationApi } from "../api/client";
import type { Report } from "../api/types";

const PAGE_SIZE = 20;

export function useModerationQueue() {
  const [reports, setReports] = useState<Report[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [hasMore, setHasMore] = useState(true);
  const offsetRef = useRef(0);
  const fetchingRef = useRef(false);

  const fetchPage = useCallback(async (offset: number, append: boolean) => {
    if (fetchingRef.current) return;
    fetchingRef.current = true;
    setLoading(true);
    setError(null);

    try {
      const data = await moderationApi.getModerationQueue(PAGE_SIZE, offset);
      const newReports = data.reports ?? [];

      setReports((prev) => (append ? [...prev, ...newReports] : newReports));
      offsetRef.current = offset + newReports.length;
      setHasMore(newReports.length >= PAGE_SIZE);
    } catch {
      setError("Failed to load moderation queue.");
    } finally {
      setLoading(false);
      fetchingRef.current = false;
    }
  }, []);

  useEffect(() => {
    fetchPage(0, false);
  }, [fetchPage]);

  const loadMore = useCallback(() => {
    if (!fetchingRef.current && hasMore) {
      fetchPage(offsetRef.current, true);
    }
  }, [fetchPage, hasMore]);

  const removeReport = useCallback((reportId: string) => {
    setReports((prev) => prev.filter((r) => r.id !== reportId));
  }, []);

  const retry = useCallback(() => {
    setReports([]);
    offsetRef.current = 0;
    setHasMore(true);
    fetchPage(0, false);
  }, [fetchPage]);

  return { reports, loading, hasMore, error, loadMore, removeReport, retry };
}
