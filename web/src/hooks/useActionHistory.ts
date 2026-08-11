import { useCallback } from "react";
import { moderationApi } from "../api/client";
import type { ActionHistoryEntry } from "../api/types";
import { DEFAULT_PAGE_SIZE, useOffsetPagination } from "./useOffsetPagination";

/**
 * An entry has no id of its own — the id belongs to the action it wraps, which
 * is why applyPage takes an accessor instead of reading `.id` off the row. An
 * entry arriving without its action is dropped rather than rendered unkeyed.
 *
 * Declared at module scope because useOffsetPagination treats it as part of its
 * reset signal.
 */
const entryKey = (entry: ActionHistoryEntry) => entry?.action?.id ?? "";

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
      entryKey,
      DEFAULT_PAGE_SIZE,
    );

  return { entries: items, loading, hasMore, error, loadMore, retry };
}
