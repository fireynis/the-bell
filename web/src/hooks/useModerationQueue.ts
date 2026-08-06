import { useCallback } from "react";
import { moderationApi } from "../api/client";
import type { ApiError, Report } from "../api/types";
import { reportResolutionOutcome, type ResolutionResult } from "../lib/moderation";
import { DEFAULT_PAGE_SIZE, useOffsetPagination } from "./useOffsetPagination";

export function useModerationQueue() {
  const fetcher = useCallback(
    async (limit: number, offset: number) => {
      const data = await moderationApi.getModerationQueue(limit, offset);
      return data.reports ?? [];
    },
    [],
  );

  const { items, loading, hasMore, error, loadMore, retry, setItems } =
    useOffsetPagination<Report>(fetcher, "Failed to load moderation queue.", DEFAULT_PAGE_SIZE);

  // Resolving a report drops it from the queue without a refetch, so the
  // moderator keeps their place in the list.
  const removeReport = useCallback(
    (reportId: string) => {
      setItems((reports) => reports.filter((r) => r.id !== reportId));
    },
    [setItems],
  );

  // Marks a report reviewed on the server and then drops it from the queue.
  //
  // Both halves matter, and only in this order. Taking action used to do the
  // second half alone: the card vanished, the row stayed `pending`, and the
  // report reappeared on the next load for a moderator who had already handled
  // it. The report is only removed from the list once the server agrees it is
  // resolved — reportResolutionOutcome decides when that is.
  const resolveReport = useCallback(
    async (reportId: string): Promise<ResolutionResult> => {
      let outcome: ResolutionResult;
      try {
        await moderationApi.updateReportStatus(reportId, "reviewed");
        outcome = reportResolutionOutcome(null);
      } catch (err) {
        outcome = reportResolutionOutcome(err as ApiError);
      }

      if (outcome.resolved) {
        removeReport(reportId);
      }
      return outcome;
    },
    [removeReport],
  );

  return {
    reports: items,
    loading,
    hasMore,
    error,
    loadMore,
    removeReport,
    resolveReport,
    retry,
  };
}
