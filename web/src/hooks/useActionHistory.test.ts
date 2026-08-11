import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { DEFAULT_PAGE_SIZE } from "./useOffsetPagination";
import { useActionHistory } from "./useActionHistory";

/**
 * An action history entry has no id of its own — the id belongs to the action
 * it wraps, at `action.id`. A Report's id is top level, so the accessor that
 * works for the moderation queue is wrong here, and wrong in a way nothing
 * would announce: every key would come back undefined, applyPage would treat
 * every entry as unkeyed, and the list would silently render empty. These
 * assert the entries survive and that repeats are recognised by the nested id.
 */

interface Entry {
  action: { id: string };
  penalties: never[];
}

/**
 * stubPagedApi serves entries from a window that gains one row at the front
 * after the first page has been read, which is what an action taken while a
 * moderator is scrolling does to the offsets.
 */
function stubPagedApi(total: number) {
  let rows: Entry[] = Array.from({ length: total }, (_, i) => ({
    action: { id: `action-${i}` },
    penalties: [],
  }));
  let served = 0;

  vi.stubGlobal(
    "fetch",
    vi.fn((url: string) => {
      const query = new URL(url, "http://test").searchParams;
      const offset = Number(query.get("offset") ?? 0);
      const limit = Number(query.get("limit") ?? 0);
      const actions = rows.slice(offset, offset + limit);
      if (served === 0) rows = [{ action: { id: "action-new" }, penalties: [] }, ...rows];
      served += 1;
      return Promise.resolve({
        ok: true,
        status: 200,
        json: () => Promise.resolve({ actions }),
      } as unknown as Response);
    }),
  );
}

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("useActionHistory", () => {
  // Reading the id off the wrong level makes every key empty, and applyPage
  // drops unkeyed rows — so a copy-pasted accessor empties the list entirely.
  it("keeps the entries it loads, whose ids are nested under the action", async () => {
    stubPagedApi(3);

    const { result } = renderHook(() => useActionHistory("user-1"));

    await waitFor(() => expect(result.current.entries).toHaveLength(3));
    expect(result.current.entries.map((e) => e.action.id)).toEqual([
      "action-0",
      "action-1",
      "action-2",
    ]);
  });

  it("does not show an entry twice when the second page overlaps the first", async () => {
    stubPagedApi(DEFAULT_PAGE_SIZE + 5);

    const { result } = renderHook(() => useActionHistory("user-1"));
    await waitFor(() => expect(result.current.entries).toHaveLength(DEFAULT_PAGE_SIZE));

    await act(async () => {
      result.current.loadMore();
    });

    await waitFor(() => expect(result.current.entries.length).toBeGreaterThan(DEFAULT_PAGE_SIZE));

    const ids = result.current.entries.map((e) => e.action.id);
    expect(new Set(ids).size).toBe(ids.length);
  });

  it("still adds the rows of the overlapping page that are new", async () => {
    stubPagedApi(DEFAULT_PAGE_SIZE + 5);

    const { result } = renderHook(() => useActionHistory("user-1"));
    await waitFor(() => expect(result.current.entries).toHaveLength(DEFAULT_PAGE_SIZE));

    await act(async () => {
      result.current.loadMore();
    });

    // Six rows came back — the shifted-down last row of page one, then the
    // five that follow it. Only the repeat is dropped.
    await waitFor(() => expect(result.current.entries).toHaveLength(DEFAULT_PAGE_SIZE + 5));
  });
});
