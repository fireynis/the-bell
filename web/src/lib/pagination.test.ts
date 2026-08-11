import { describe, expect, it } from "vitest";
import { applyPage, initialPageState, type PageState } from "./pagination";

const PAGE_SIZE = 3;

/** page returns n placeholder rows, numbered from `from` so they stay distinct. */
function page(n: number, from = 0): string[] {
  return Array.from({ length: n }, (_, i) => `item-${from + i}`);
}

/** The rows in these cases are their own key. */
const idOf = (item: string) => item;

function loaded(items: string[], offset = items.length, hasMore = true): PageState<string> {
  return { items, offset, hasMore };
}

describe("initialPageState", () => {
  it("starts empty at offset zero but willing to fetch", () => {
    expect(initialPageState()).toEqual({ items: [], offset: 0, hasMore: true });
  });

  it("hands out a fresh object each time, so a retry cannot reuse stale items", () => {
    initialPageState<string>().items.push("stale");
    expect(initialPageState().items).toEqual([]);
  });
});

describe("applyPage", () => {
  it("appends a page after the items already loaded", () => {
    const state = applyPage(initialPageState<string>(), page(3), PAGE_SIZE, true, idOf);
    const got = applyPage(state, page(2, 3), PAGE_SIZE, true, idOf);
    expect(got.items).toEqual(["item-0", "item-1", "item-2", "item-3", "item-4"]);
  });

  it("replaces the items when not appending, so a reload starts clean", () => {
    const got = applyPage(loaded(page(3)), page(1, 9), PAGE_SIZE, false, idOf);
    expect(got.items).toEqual(["item-9"]);
  });

  // A full page is the only signal that more may exist; getting this boundary
  // wrong either drops the last page or fetches one that is always empty.
  it("keeps paging when the page is exactly full", () => {
    expect(
      applyPage(initialPageState<string>(), page(PAGE_SIZE), PAGE_SIZE, true, idOf).hasMore,
    ).toBe(true);
  });

  it("stops paging one item short of a full page", () => {
    expect(
      applyPage(initialPageState<string>(), page(PAGE_SIZE - 1), PAGE_SIZE, true, idOf).hasMore,
    ).toBe(false);
  });

  it("stops paging on an empty page", () => {
    expect(applyPage(loaded(page(3)), [], PAGE_SIZE, true, idOf).hasMore).toBe(false);
  });

  // A server that over-delivers must still terminate rather than page forever.
  it("keeps paging when the server returns more than asked for", () => {
    expect(
      applyPage(initialPageState<string>(), page(PAGE_SIZE + 1), PAGE_SIZE, true, idOf).hasMore,
    ).toBe(true);
  });

  it("advances the offset by the number of items received", () => {
    const first = applyPage(initialPageState<string>(), page(3), PAGE_SIZE, true, idOf);
    expect(first.offset).toBe(3);
    expect(applyPage(first, page(2, 3), PAGE_SIZE, true, idOf).offset).toBe(5);
  });

  // Advancing by the page size instead would skip rows whenever the server
  // returned fewer than it was asked for.
  it("advances by what arrived, not by the page size", () => {
    const got = applyPage(loaded(page(3)), page(1, 3), PAGE_SIZE, true, idOf);
    expect(got.offset).toBe(4);
  });

  it("resets the offset when replacing", () => {
    const got = applyPage(loaded(page(9), 9), page(2), PAGE_SIZE, false, idOf);
    expect(got.offset).toBe(2);
  });

  it("leaves the items alone when an empty page is appended", () => {
    const got = applyPage(loaded(page(3)), [], PAGE_SIZE, true, idOf);
    expect(got.items).toEqual(page(3));
    expect(got.offset).toBe(3);
  });

  it("empties the list when a reload comes back with nothing", () => {
    const got = applyPage(loaded(page(3)), [], PAGE_SIZE, false, idOf);
    expect(got).toEqual({ items: [], offset: 0, hasMore: false });
  });

  it.each([
    ["a missing page", undefined],
    ["a null page", null],
  ])("treats %s as empty rather than throwing", (_label, input) => {
    const got = applyPage(loaded(page(3)), input, PAGE_SIZE, true, idOf);
    expect(got.items).toEqual(page(3));
    expect(got.hasMore).toBe(false);
  });

  it("does not mutate the state it was given", () => {
    const state = loaded(page(3));
    applyPage(state, page(2, 3), PAGE_SIZE, true, idOf);
    expect(state.items).toEqual(page(3));
    expect(state.offset).toBe(3);
  });
});

/**
 * Offset paging is not stable. A row inserted ahead of the window between two
 * requests shifts every later row down by one, so the next page re-delivers
 * rows the list already has. Without deduping they render twice and React warns
 * about a duplicate key.
 */
describe("applyPage deduping", () => {
  it("does not repeat a row that an earlier page already delivered", () => {
    const state = applyPage(initialPageState<string>(), page(3), PAGE_SIZE, true, idOf);
    // The next page overlaps by one, as it does when a row is inserted mid-scroll.
    const got = applyPage(state, page(3, 2), PAGE_SIZE, true, idOf);
    expect(got.items).toEqual(["item-0", "item-1", "item-2", "item-3", "item-4"]);
  });

  it("still appends the genuinely new rows of an overlapping page", () => {
    const got = applyPage(loaded(page(3)), page(3, 2), PAGE_SIZE, true, idOf);
    expect(got.items).toHaveLength(5);
    expect(got.items.slice(3)).toEqual(["item-3", "item-4"]);
  });

  it("drops a duplicate that arrives twice inside a single page", () => {
    const got = applyPage(loaded(page(1)), ["item-1", "item-1", "item-2"], PAGE_SIZE, true, idOf);
    expect(got.items).toEqual(["item-0", "item-1", "item-2"]);
  });

  // The decision that governs whether one more fetch happens has to read the
  // rows the server sent, not the rows this list chose to keep: a full page of
  // duplicates still means the server has more behind it.
  it("keeps paging when a page of exactly PAGE_SIZE is entirely duplicates", () => {
    const got = applyPage(loaded(page(PAGE_SIZE)), page(PAGE_SIZE), PAGE_SIZE, true, idOf);
    expect(got.items).toEqual(page(PAGE_SIZE));
    expect(got.hasMore).toBe(true);
  });

  it("stops paging on a short page even when none of it was a duplicate", () => {
    const got = applyPage(loaded(page(3)), page(PAGE_SIZE - 1, 3), PAGE_SIZE, true, idOf);
    expect(got.hasMore).toBe(false);
  });

  // Advancing by the rows kept would make the next request ask for rows this
  // one has already read and discarded, and the list would never move on.
  it("advances the offset past the duplicates, not just past the rows kept", () => {
    const got = applyPage(loaded(page(3)), page(3, 2), PAGE_SIZE, true, idOf);
    expect(got.offset).toBe(6);
  });

  it("dedupes a reload against itself but not against the rows it replaces", () => {
    const got = applyPage(loaded(page(3)), ["item-0", "item-0"], PAGE_SIZE, false, idOf);
    expect(got.items).toEqual(["item-0"]);
  });

  it("compares the accessor's key, not object identity, so a re-sent row is caught", () => {
    // Two distinct objects for the same entry, as two responses would produce.
    const first = { action: { id: "a1" }, penalties: [] };
    const again = { action: { id: "a1" }, penalties: [] };
    const nested = (entry: { action: { id: string } }) => entry.action.id;

    const state = applyPage(initialPageState<typeof first>(), [first], PAGE_SIZE, true, nested);
    const got = applyPage(state, [again], PAGE_SIZE, true, nested);

    expect(got.items).toEqual([first]);
  });

  // A row with no key can be neither deduped nor rendered, so a malformed page
  // costs that row rather than the whole render — as appendFeedPage does.
  it.each([
    ["an empty key", ""],
    ["a missing key", undefined as unknown as string],
  ])("drops a row with %s", (_label, key) => {
    const rows = [{ id: key }, { id: "ok" }];
    const got = applyPage(
      initialPageState<{ id: string }>(),
      rows,
      PAGE_SIZE,
      true,
      (row) => row.id,
    );
    expect(got.items).toEqual([{ id: "ok" }]);
  });
});
