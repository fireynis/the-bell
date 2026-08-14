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
 *
 * keyOf identifies a row so applyPage can drop one that a later page
 * re-delivers, which offset paging does whenever a row is inserted ahead of the
 * window. It must be stable for the same reason the fetcher must: it is part of
 * the same effect, so an inline arrow would reload the first page forever.
 * Declare it at module scope.
 */
export function useOffsetPagination<T>(
  fetcher: PageFetcher<T>,
  errorMessage: string,
  keyOf: (item: T) => string,
  pageSize: number = DEFAULT_PAGE_SIZE,
): OffsetPagination<T> {
  const [state, setState] = useState<PageState<T>>(initialPageState<T>);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const fetchingRef = useRef(false);
  /**
   * Which request owns the state. Incremented by every request, so one that
   * has been superseded can recognise itself when it finally answers and say
   * nothing — the responses are not guaranteed to come back in the order they
   * were sent, and the older one landing last would leave the list showing an
   * answer to a question nobody is asking any more.
   */
  const generationRef = useRef(0);

  const fetchPage = useCallback(
    async (offset: number, append: boolean) => {
      // An append arriving while a request is in flight is a duplicate of it —
      // two intersection-observer firings for the same scroll — so it is
      // dropped. A reset is not: it is a different question, asked because the
      // subject or the search changed, and dropping it used to leave the list
      // answering the previous one with nothing scheduled to correct it.
      if (fetchingRef.current && append) return;

      const generation = generationRef.current + 1;
      generationRef.current = generation;
      fetchingRef.current = true;
      setLoading(true);
      setError(null);

      // Clearing first means a reload cannot leave the previous subject's rows
      // on screen while the new ones are in flight.
      if (!append) setState(initialPageState<T>());

      try {
        const items = await fetcher(pageSize, offset);
        if (generation !== generationRef.current) return;
        setState((prev) => applyPage(prev, items, pageSize, append, keyOf));
      } catch {
        if (generation !== generationRef.current) return;
        setError(errorMessage);
      } finally {
        // Only the request that still owns the state may end the loading it
        // started; a superseded one leaves both flags to its successor, which
        // is still in flight.
        if (generation === generationRef.current) {
          setLoading(false);
          fetchingRef.current = false;
        }
      }
    },
    [fetcher, errorMessage, keyOf, pageSize],
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
