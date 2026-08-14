import { useCallback, useRef, useState } from "react";
import { userApi } from "../api/client";
import type { DirectoryUser } from "../api/types";
import { useOffsetPagination } from "./useOffsetPagination";

/** A page of the directory. Twenty-five names is about a screen and a half. */
export const NEIGHBORS_PAGE_SIZE = 25;

/**
 * Declared at module scope because useOffsetPagination folds it into its reset
 * signal — an inline arrow would reload the first page forever.
 */
const neighborKey = (neighbor: DirectoryUser) => neighbor?.id ?? "";

export interface Neighbors {
  neighbors: DirectoryUser[];
  /**
   * How many members match the search in total, or null before the first
   * response has arrived. Null rather than 0 because "none yet" and "none at
   * all" are different things to say to somebody searching.
   */
  total: number | null;
  loading: boolean;
  hasMore: boolean;
  error: string | null;
  loadMore: () => void;
  retry: () => void;
}

/**
 * useNeighbors reads the member directory, one page at a time, for one search.
 *
 * The query is part of the fetcher's identity, so changing it resets the list
 * rather than appending the results for "ali" onto the results for "bo". Pass a
 * debounced value: this fires a request for every distinct query it is handed.
 *
 * `hasMore` is the pagination hook's own end-of-list signal narrowed by the
 * server's total. The signal alone ends the list a page late — a full final
 * page looks like there may be more, and it takes an empty request to find out
 * — while the total alone can never end it at all, since a row deduplicated out
 * of a page leaves the loaded count permanently short of the total. Narrowing
 * one by the other ends the list as soon as either says so, so the button
 * cannot be offered for a page that would arrive empty.
 */
export function useNeighbors(query: string): Neighbors {
  const [total, setTotal] = useState<number | null>(null);
  /**
   * Which request the total belongs to. The rows are guarded by the pagination
   * hook's own bookkeeping, but the total is read here and would otherwise
   * escape it: a superseded response landing last would leave the count of one
   * search sitting above the results of another.
   */
  const requestRef = useRef(0);

  const fetcher = useCallback(
    async (limit: number, offset: number) => {
      const generation = requestRef.current + 1;
      requestRef.current = generation;

      const data = await userApi.list(limit, offset, query);
      if (generation === requestRef.current) {
        setTotal(typeof data?.total === "number" ? data.total : null);
      }
      return data?.users ?? [];
    },
    [query],
  );

  const { items, loading, hasMore, error, loadMore, retry } = useOffsetPagination<DirectoryUser>(
    fetcher,
    "We could not load the directory just now.",
    neighborKey,
    NEIGHBORS_PAGE_SIZE,
  );

  return {
    neighbors: items,
    total,
    loading,
    hasMore: hasMore && (total === null || items.length < total),
    error,
    loadMore,
    retry,
  };
}
