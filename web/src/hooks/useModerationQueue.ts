import { useCallback } from "react";
import { moderationApi } from "../api/client";
import type { Report } from "../api/types";
import { DEFAULT_PAGE_SIZE, useOffsetPagination } from "./useOffsetPagination";

export function useModerationQueue() {
  const fetcher = useCallback(
    async (limit: number, offset: number) => {
      const data = await moderationApi.getModerationQueue(limit, offset);
      return data.reports ?? [];
    },
    [],
  );

  const { items, loading, hasMore, error, loadMore, retry, setItems } =
    useOffsetPagination<Report>(fetcher, "Failed to load moderation queue.", DEFAULT_PAGE_SIZE);

  // Resolving a report drops it from the queue without a refetch, so the
  // moderator keeps their place in the list.
  const removeReport = useCallback(
    (reportId: string) => {
      setItems((reports) => reports.filter((r) => r.id !== reportId));
    },
    [setItems],
  );

  return { reports: items, loading, hasMore, error, loadMore, removeReport, retry };
}
