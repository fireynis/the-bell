/** Everything an offset-paginated list needs to render and to ask for more. */
export interface PageState<T> {
  items: T[];
  /** Offset to request next; equal to the number of items received so far. */
  offset: number;
  hasMore: boolean;
}

/**
 * initialPageState starts with hasMore true so the first page is fetched;
 * only a short page from the server can say the list is exhausted.
 */
export function initialPageState<T>(): PageState<T> {
  return { items: [], offset: 0, hasMore: true };
}

/**
 * applyPage folds a page of results into the list, appending when paging and
 * replacing when reloading from the start.
 *
 * A full page means there may be more; a short one means there is not. That
 * comparison is `>= pageSize` rather than `=== pageSize` so a server that
 * over-delivers still ends the list correctly instead of paging forever.
 *
 * The offset advances by however many items actually arrived, not by the page
 * size, so a server returning fewer than asked for cannot make the next request
 * skip over rows.
 *
 * A malformed or missing page is treated as empty, which ends the list rather
 * than throwing part-way through a render.
 */
export function applyPage<T>(
  state: PageState<T>,
  page: readonly T[] | undefined | null,
  pageSize: number,
  append: boolean,
): PageState<T> {
  const incoming = Array.isArray(page) ? page : [];
  const base = append ? state.items : [];

  return {
    items: [...base, ...incoming],
    offset: (append ? state.offset : 0) + incoming.length,
    hasMore: incoming.length >= pageSize,
  };
}
