import { describe, expect, it } from "vitest";
import { applyPage, initialPageState, type PageState } from "./pagination";

const PAGE_SIZE = 3;

/** page returns n placeholder rows, numbered from `from` so they stay distinct. */
function page(n: number, from = 0): string[] {
  return Array.from({ length: n }, (_, i) => `item-${from + i}`);
}

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
    const state = applyPage(initialPageState<string>(), page(3), PAGE_SIZE, true);
    const got = applyPage(state, page(2, 3), PAGE_SIZE, true);
    expect(got.items).toEqual(["item-0", "item-1", "item-2", "item-3", "item-4"]);
  });

  it("replaces the items when not appending, so a reload starts clean", () => {
    const got = applyPage(loaded(page(3)), page(1, 9), PAGE_SIZE, false);
    expect(got.items).toEqual(["item-9"]);
  });

  // A full page is the only signal that more may exist; getting this boundary
  // wrong either drops the last page or fetches one that is always empty.
  it("keeps paging when the page is exactly full", () => {
    expect(applyPage(initialPageState<string>(), page(PAGE_SIZE), PAGE_SIZE, true).hasMore).toBe(
      true,
    );
  });

  it("stops paging one item short of a full page", () => {
    expect(
      applyPage(initialPageState<string>(), page(PAGE_SIZE - 1), PAGE_SIZE, true).hasMore,
    ).toBe(false);
  });

  it("stops paging on an empty page", () => {
    expect(applyPage(loaded(page(3)), [], PAGE_SIZE, true).hasMore).toBe(false);
  });

  // A server that over-delivers must still terminate rather than page forever.
  it("keeps paging when the server returns more than asked for", () => {
    expect(
      applyPage(initialPageState<string>(), page(PAGE_SIZE + 1), PAGE_SIZE, true).hasMore,
    ).toBe(true);
  });

  it("advances the offset by the number of items received", () => {
    const first = applyPage(initialPageState<string>(), page(3), PAGE_SIZE, true);
    expect(first.offset).toBe(3);
    expect(applyPage(first, page(2, 3), PAGE_SIZE, true).offset).toBe(5);
  });

  // Advancing by the page size instead would skip rows whenever the server
  // returned fewer than it was asked for.
  it("advances by what arrived, not by the page size", () => {
    const got = applyPage(loaded(page(3)), page(1, 3), PAGE_SIZE, true);
    expect(got.offset).toBe(4);
  });

  it("resets the offset when replacing", () => {
    const got = applyPage(loaded(page(9), 9), page(2), PAGE_SIZE, false);
    expect(got.offset).toBe(2);
  });

  it("leaves the items alone when an empty page is appended", () => {
    const got = applyPage(loaded(page(3)), [], PAGE_SIZE, true);
    expect(got.items).toEqual(page(3));
    expect(got.offset).toBe(3);
  });

  it("empties the list when a reload comes back with nothing", () => {
    const got = applyPage(loaded(page(3)), [], PAGE_SIZE, false);
    expect(got).toEqual({ items: [], offset: 0, hasMore: false });
  });

  it.each([
    ["a missing page", undefined],
    ["a null page", null],
  ])("treats %s as empty rather than throwing", (_label, input) => {
    const got = applyPage(loaded(page(3)), input, PAGE_SIZE, true);
    expect(got.items).toEqual(page(3));
    expect(got.hasMore).toBe(false);
  });

  it("does not mutate the state it was given", () => {
    const state = loaded(page(3));
    applyPage(state, page(2, 3), PAGE_SIZE, true);
    expect(state.items).toEqual(page(3));
    expect(state.offset).toBe(3);
  });
});
