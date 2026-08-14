import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { DirectoryUser } from "../api/types";
import { NEIGHBORS_PAGE_SIZE, useNeighbors } from "./useNeighbors";

/**
 * A search box changes the query while the previous request may still be in
 * flight, which no other consumer of useOffsetPagination does — the moderation
 * queue and the action history only reset on navigation. Two failures follow
 * from that and both leave the list quietly wrong rather than broken: a reset
 * dropped because a request was in flight, and a superseded response landing
 * last. These pin both.
 */

function row(id: string): DirectoryUser {
  return { id, display_name: id, role: "member", joined_at: "2026-03-01T12:00:00Z" };
}

interface Pending {
  url: string;
  answer: (body: unknown) => void;
}

/** A fetch that answers nothing until a test says so, in the order it chooses. */
function deferredApi(): Pending[] {
  const pending: Pending[] = [];

  vi.stubGlobal(
    "fetch",
    vi.fn(
      (url: string) =>
        new Promise<Response>((resolve) => {
          pending.push({
            url,
            answer: (body) =>
              resolve({
                ok: true,
                status: 200,
                json: () => Promise.resolve(body),
              } as unknown as Response),
          });
        }),
    ),
  );

  return pending;
}

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("useNeighbors with a search in flight", () => {
  it("asks again for the new search even though the old request has not answered", async () => {
    const pending = deferredApi();
    const { rerender } = renderHook(({ q }) => useNeighbors(q), {
      initialProps: { q: "ali" },
    });
    await waitFor(() => expect(pending).toHaveLength(1));

    rerender({ q: "alice" });

    // Dropped, the list would answer "ali" for as long as the page is open.
    await waitFor(() => expect(pending).toHaveLength(2));
    expect(pending[1].url).toContain("q=alice");
  });

  it("ignores the older response when it lands after the newer one", async () => {
    const pending = deferredApi();
    const { result, rerender } = renderHook(({ q }) => useNeighbors(q), {
      initialProps: { q: "ali" },
    });
    await waitFor(() => expect(pending).toHaveLength(1));

    rerender({ q: "alice" });
    await waitFor(() => expect(pending).toHaveLength(2));

    await act(async () => {
      pending[1].answer({ users: [row("alice")], total: 1 });
    });
    await act(async () => {
      pending[0].answer({ users: [row("ali-1"), row("ali-2")], total: 9 });
    });

    await waitFor(() => expect(result.current.neighbors.map((n) => n.id)).toEqual(["alice"]));
  });

  // The count sits directly above the rows, so a total from a different search
  // is a caption disagreeing with its own list.
  it("keeps the total from the search that is actually showing", async () => {
    const pending = deferredApi();
    const { result, rerender } = renderHook(({ q }) => useNeighbors(q), {
      initialProps: { q: "ali" },
    });
    await waitFor(() => expect(pending).toHaveLength(1));

    rerender({ q: "alice" });
    await waitFor(() => expect(pending).toHaveLength(2));

    await act(async () => {
      pending[1].answer({ users: [row("alice")], total: 1 });
    });
    await act(async () => {
      pending[0].answer({ users: [row("ali-1")], total: 9 });
    });

    await waitFor(() => expect(result.current.total).toBe(1));
  });

  it("stops loading once the search that is showing has answered", async () => {
    const pending = deferredApi();
    const { result, rerender } = renderHook(({ q }) => useNeighbors(q), {
      initialProps: { q: "ali" },
    });
    await waitFor(() => expect(pending).toHaveLength(1));

    rerender({ q: "alice" });
    await waitFor(() => expect(pending).toHaveLength(2));

    await act(async () => {
      pending[1].answer({ users: [row("alice")], total: 1 });
    });

    await waitFor(() => expect(result.current.loading).toBe(false));
  });
});

describe("useNeighbors paging", () => {
  /** Answers immediately from a fixed roll of `total` members. */
  function stubRoll(total: number) {
    vi.stubGlobal(
      "fetch",
      vi.fn((url: string) => {
        const params = new URL(url, "http://test").searchParams;
        const offset = Number(params.get("offset") ?? 0);
        const limit = Number(params.get("limit") ?? 0);
        const users = Array.from({ length: total }, (_, i) => row(`user-${i}`)).slice(
          offset,
          offset + limit,
        );
        return Promise.resolve({
          ok: true,
          status: 200,
          json: () => Promise.resolve({ users, total }),
        } as unknown as Response);
      }),
    );
  }

  // A full final page looks like there may be more; the server's total is what
  // says otherwise, and offering a button that fetches nothing is worse than
  // offering none.
  it("does not offer more when the first page is the whole roll", async () => {
    stubRoll(NEIGHBORS_PAGE_SIZE);

    const { result } = renderHook(() => useNeighbors(""));

    await waitFor(() => expect(result.current.neighbors).toHaveLength(NEIGHBORS_PAGE_SIZE));
    expect(result.current.hasMore).toBe(false);
  });

  it("offers more while the roll is longer than what is loaded", async () => {
    stubRoll(NEIGHBORS_PAGE_SIZE + 1);

    const { result } = renderHook(() => useNeighbors(""));

    await waitFor(() => expect(result.current.neighbors).toHaveLength(NEIGHBORS_PAGE_SIZE));
    expect(result.current.hasMore).toBe(true);
  });

  it("appends the next page and then stops offering more", async () => {
    stubRoll(NEIGHBORS_PAGE_SIZE + 3);

    const { result } = renderHook(() => useNeighbors(""));
    await waitFor(() => expect(result.current.neighbors).toHaveLength(NEIGHBORS_PAGE_SIZE));

    await act(async () => {
      result.current.loadMore();
    });

    await waitFor(() => expect(result.current.neighbors).toHaveLength(NEIGHBORS_PAGE_SIZE + 3));
    expect(result.current.hasMore).toBe(false);
  });
});
