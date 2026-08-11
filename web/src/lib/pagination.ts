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
 * Rows already on screen are dropped from the incoming page. Offset paging is
 * not stable: a row inserted ahead of the window between two requests shifts
 * every later row down by one, so the next page re-delivers the last row of the
 * previous one and it renders twice, with React warning about a duplicate key.
 * appendFeedPage in post.ts has always done this for the cursor-paged feed;
 * these lists went without because there was no uniform key to dedupe on — a
 * Report carries a top-level `id`, an ActionHistoryEntry's id lives at
 * `action.id`. Hence keyOf: the caller names the key rather than this having to
 * guess at a field.
 *
 * Only `items` is deduped. `offset` still advances by however many rows the
 * server sent and `hasMore` still reads that same raw count, because both
 * describe the server's window rather than this list: counting only the rows
 * kept would make the next request re-read the ones just discarded, and would
 * end the list early on a full page that happened to be all duplicates.
 *
 * A row whose key is missing or empty is dropped rather than kept unkeyed,
 * matching appendFeedPage — an unkeyed row is one this list cannot dedupe or
 * render safely, and a malformed page should cost that row, not the render.
 *
 * A malformed or missing page is treated as empty, which ends the list rather
 * than throwing part-way through a render.
 */
export function applyPage<T>(
  state: PageState<T>,
  page: readonly T[] | undefined | null,
  pageSize: number,
  append: boolean,
  keyOf: (item: T) => string,
): PageState<T> {
  const incoming = Array.isArray(page) ? page : [];
  const base = append ? state.items : [];

  const seen = new Set(base.map((item) => keyOf(item)));
  const items = [...base];

  for (const item of incoming) {
    const key = keyOf(item);
    if (typeof key !== "string" || key === "" || seen.has(key)) continue;
    seen.add(key);
    items.push(item);
  }

  return {
    items,
    offset: (append ? state.offset : 0) + incoming.length,
    hasMore: incoming.length >= pageSize,
  };
}
