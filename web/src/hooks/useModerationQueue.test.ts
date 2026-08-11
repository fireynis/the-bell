import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { DEFAULT_PAGE_SIZE } from "./useOffsetPagination";
import { useModerationQueue } from "./useModerationQueue";

/**
 * These pin the half of resolving a report that used to be missing entirely.
 *
 * The queue dropped a report from local state and never told the server, so the
 * row stayed `pending` and the report came back on the next load for a
 * moderator who had already handled it. Nothing noticed, because nothing
 * asserted that the PATCH was ever issued. That is what these assert.
 *
 * fetch is stubbed, so these prove this hook's behaviour rather than the wire
 * contract; internal/handler/report.go owns the contract and its own tests.
 */

interface Call {
  url: string;
  init: RequestInit;
}

/**
 * stubApi answers the queue load with one report and every PATCH with
 * patchResponse, recording each call so a test can assert what was sent.
 */
function stubApi(patchResponse: { ok: boolean; status: number; body: unknown }) {
  const calls: Call[] = [];

  const fetchMock = vi.fn((url: string, init: RequestInit = {}) => {
    calls.push({ url, init });

    if ((init.method ?? "GET") === "GET") {
      return Promise.resolve({
        ok: true,
        status: 200,
        json: () => Promise.resolve({ reports: [{ id: "report-1", post_id: "post-1" }] }),
      } as unknown as Response);
    }

    return Promise.resolve({
      ok: patchResponse.ok,
      status: patchResponse.status,
      json: () => Promise.resolve(patchResponse.body),
    } as unknown as Response);
  });

  vi.stubGlobal("fetch", fetchMock);
  return calls;
}

const patchCalls = (calls: Call[]) => calls.filter((c) => c.init.method === "PATCH");

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("useModerationQueue.resolveReport", () => {
  it("marks the report reviewed on the server, not just in local state", async () => {
    const calls = stubApi({ ok: true, status: 200, body: { id: "report-1", status: "reviewed" } });

    const { result } = renderHook(() => useModerationQueue());
    await waitFor(() => expect(result.current.reports).toHaveLength(1));

    const outcome = await result.current.resolveReport("report-1");

    expect(outcome.resolved).toBe(true);
    expect(patchCalls(calls)).toHaveLength(1);
    expect(patchCalls(calls)[0].url).toBe("/api/v1/moderation/reports/report-1");
    expect(JSON.parse(patchCalls(calls)[0].init.body as string)).toEqual({ status: "reviewed" });

    await waitFor(() => expect(result.current.reports).toHaveLength(0));
  });

  // The action already succeeded. Dropping the report anyway is exactly the
  // divergence this closes: the server still has it pending, so the queue must
  // keep showing it.
  it("keeps the report in the queue when the server refuses to resolve it", async () => {
    const calls = stubApi({ ok: false, status: 500, body: { error: "internal error" } });

    const { result } = renderHook(() => useModerationQueue());
    await waitFor(() => expect(result.current.reports).toHaveLength(1));

    const outcome = await result.current.resolveReport("report-1");

    expect(patchCalls(calls)).toHaveLength(1);
    expect(outcome.resolved).toBe(false);
    expect(outcome.error).toContain("internal error");
    expect(result.current.reports).toHaveLength(1);
  });

  // Another moderator resolved it first. The report is gone either way, so
  // holding it in this moderator's queue helps nobody.
  it("drops the report when the server says it is already gone", async () => {
    stubApi({ ok: false, status: 404, body: { error: "not found" } });

    const { result } = renderHook(() => useModerationQueue());
    await waitFor(() => expect(result.current.reports).toHaveLength(1));

    const outcome = await result.current.resolveReport("report-1");

    expect(outcome.resolved).toBe(true);
    await waitFor(() => expect(result.current.reports).toHaveLength(0));
  });
});

/**
 * Offset paging is not stable: a report filed while a moderator is scrolling
 * shifts every row down one, so the next page re-delivers the last row of the
 * previous one. applyPage drops the repeat, but only if the queue hands it a
 * key to recognise rows by — this asserts that wiring, not applyPage itself.
 */
describe("useModerationQueue paging over a moving queue", () => {
  /**
   * stubPagedApi serves reports from a window that gains one row at the front
   * after the first page has been read, which is exactly what a report filed
   * mid-scroll does to the offsets.
   */
  function stubPagedApi(total: number) {
    let rows = Array.from({ length: total }, (_, i) => ({ id: `report-${i}`, post_id: `post-${i}` }));
    let served = 0;

    vi.stubGlobal(
      "fetch",
      vi.fn((url: string) => {
        const offset = Number(new URL(url, "http://test").searchParams.get("offset") ?? 0);
        const limit = Number(new URL(url, "http://test").searchParams.get("limit") ?? 0);
        const reports = rows.slice(offset, offset + limit);
        if (served === 0) rows = [{ id: "report-new", post_id: "post-new" }, ...rows];
        served += 1;
        return Promise.resolve({
          ok: true,
          status: 200,
          json: () => Promise.resolve({ reports }),
        } as unknown as Response);
      }),
    );
  }

  it("does not show a report twice when the second page overlaps the first", async () => {
    stubPagedApi(DEFAULT_PAGE_SIZE + 5);

    const { result } = renderHook(() => useModerationQueue());
    await waitFor(() => expect(result.current.reports).toHaveLength(DEFAULT_PAGE_SIZE));

    await act(async () => {
      result.current.loadMore();
    });

    await waitFor(() => expect(result.current.reports.length).toBeGreaterThan(DEFAULT_PAGE_SIZE));

    const ids = result.current.reports.map((r) => r.id);
    expect(new Set(ids).size).toBe(ids.length);
  });

  it("still adds the rows of the overlapping page that are new", async () => {
    stubPagedApi(DEFAULT_PAGE_SIZE + 5);

    const { result } = renderHook(() => useModerationQueue());
    await waitFor(() => expect(result.current.reports).toHaveLength(DEFAULT_PAGE_SIZE));

    await act(async () => {
      result.current.loadMore();
    });

    // Six rows came back — the shifted-down last row of page one, then the
    // five that follow it. Only the repeat is dropped.
    await waitFor(() => expect(result.current.reports).toHaveLength(DEFAULT_PAGE_SIZE + 5));
  });
});
