import { useCallback, useEffect, useRef, useState } from "react";
import { applyPage, initialPageState, type PageState } from "../lib/pagination";

export const DEFAULT_PAGE_SIZE = 20;

/** Fetches one page; the hook supplies the limit and offset. */
export type PageFetcher<T> = (limit: number, offset: number) => Promise<readonly T[]>;

export interface OffsetPagination<T> {
  items: T[];
  loading: boolean;
  hasMore: boolean;
  error: string | null;
  loadMore: () => void;
  retry: () => void;
  /** Edit the loaded items in place, for optimistic removals. */
  setItems: (update: (items: T[]) => T[]) => void;
}

/**
 * useOffsetPagination drives an infinite-scrolling list backed by limit/offset
 * paging.
 *
 * The fetcher is expected to be memoised by the caller: it is the hook's reset
 * signal, so a fetcher that changes identity every render would reload the
 * first page forever, and one that never changes when its inputs do would keep
 * showing the previous subject's rows.
 */
export function useOffsetPagination<T>(
  fetcher: PageFetcher<T>,
  errorMessage: string,
  pageSize: number = DEFAULT_PAGE_SIZE,
): OffsetPagination<T> {
  const [state, setState] = useState<PageState<T>>(initialPageState<T>);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const fetchingRef = useRef(false);

  const fetchPage = useCallback(
    async (offset: number, append: boolean) => {
      if (fetchingRef.current) return;
      fetchingRef.current = true;
      setLoading(true);
      setError(null);

      // Clearing first means a reload cannot leave the previous subject's rows
      // on screen while the new ones are in flight.
      if (!append) setState(initialPageState<T>());

      try {
        const items = await fetcher(pageSize, offset);
        setState((prev) => applyPage(prev, items, pageSize, append));
      } catch {
        setError(errorMessage);
      } finally {
        setLoading(false);
        fetchingRef.current = false;
      }
    },
    [fetcher, errorMessage, pageSize],
  );

  useEffect(() => {
    fetchPage(0, false);
  }, [fetchPage]);

  const loadMore = useCallback(() => {
    if (!fetchingRef.current && state.hasMore) {
      fetchPage(state.offset, true);
    }
  }, [fetchPage, state.hasMore, state.offset]);

  const retry = useCallback(() => {
    fetchPage(0, false);
  }, [fetchPage]);

  const setItems = useCallback((update: (items: T[]) => T[]) => {
    setState((prev) => ({ ...prev, items: update(prev.items) }));
  }, []);

  return {
    items: state.items,
    loading,
    hasMore: state.hasMore,
    error,
    loadMore,
    retry,
    setItems,
  };
}
