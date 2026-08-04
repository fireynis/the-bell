import { useCallback } from "react";
import { moderationApi } from "../api/client";
import type { ActionHistoryEntry } from "../api/types";
import { DEFAULT_PAGE_SIZE, useOffsetPagination } from "./useOffsetPagination";

export function useActionHistory(userId: string) {
  // Depends on userId so that viewing a different user resets the list rather
  // than appending their history onto the previous user's.
  const fetcher = useCallback(
    async (limit: number, offset: number) => {
      const data = await moderationApi.getActionHistory(userId, limit, offset);
      return data.actions ?? [];
    },
    [userId],
  );

  const { items, loading, hasMore, error, loadMore, retry } =
    useOffsetPagination<ActionHistoryEntry>(
      fetcher,
      "Failed to load action history.",
      DEFAULT_PAGE_SIZE,
    );

  return { entries: items, loading, hasMore, error, loadMore, retry };
}
