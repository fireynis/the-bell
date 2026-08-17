import { useCallback, useRef, useState } from "react";
import { approvalApi } from "../api/client";
import type { ApiError, User } from "../api/types";
import { APPROVALS_LOAD_ERROR, APPROVALS_PAGE_SIZE } from "../lib/approvals";
import { useOffsetPagination } from "./useOffsetPagination";

/**
 * Declared at module scope because useOffsetPagination folds it into its reset
 * signal — an inline arrow would reload the first page forever.
 */
const applicantKey = (applicant: User) => applicant?.id ?? "";

export interface PendingApprovals {
  applicants: User[];
  /**
   * How many applicants match the search in total, or null before the first
   * response has arrived. Null rather than 0 because "none yet" and "none at
   * all" are different things to say to somebody working through a queue.
   */
  total: number | null;
  loading: boolean;
  hasMore: boolean;
  error: string | null;
  /**
   * True when the town has left bootstrap mode, which the server reports by
   * refusing the queue with a 403.
   *
   * It is not a failure and must not be shown as one. The twentieth approval
   * ends bootstrap mode — possibly one the council member reading this page
   * just made — and from then on residents are admitted by vouch rather than by
   * the council. A page that said "we could not load the queue" would send
   * somebody looking for an outage that is actually the town growing up.
   */
  closed: boolean;
  loadMore: () => void;
  retry: () => void;
  /** Drops an approved applicant from the list and from the count. */
  removeApproved: (userId: string) => void;
}

/**
 * usePendingApprovals reads the council's approval queue, one page at a time,
 * for one search.
 *
 * The query is part of the fetcher's identity, so changing it resets the list
 * rather than appending the results for "ali" onto the results for "bo". Pass a
 * debounced value: this fires a request for every distinct query it is handed.
 *
 * `hasMore` narrows the pagination hook's end-of-list signal by the server's
 * total, for the reason given on useNeighbors — either saying so ends the list,
 * so the button cannot be offered for a page that would arrive empty.
 *
 * An approval removes its applicant here rather than reloading the queue. The
 * reload is what the dashboard used to do, and it is wrong for a paged list:
 * somebody who has scrolled to the fourth page and approves one person would
 * be thrown back to the first. Dropping the row and the count keeps the page
 * they are working on, and any drift from a second council member working the
 * same queue is corrected by the next page they load.
 */
export function usePendingApprovals(query: string): PendingApprovals {
  const [total, setTotal] = useState<number | null>(null);
  const [closed, setClosed] = useState(false);
  /**
   * Which request the total belongs to. The rows are guarded by the pagination
   * hook's own bookkeeping, but the total is read here and would otherwise
   * escape it: a superseded response landing last would leave the count of one
   * search sitting above the results of another.
   */
  const requestRef = useRef(0);

  const fetcher = useCallback(
    async (limit: number, offset: number) => {
      const generation = requestRef.current + 1;
      requestRef.current = generation;

      try {
        const data = await approvalApi.listPending(limit, offset, query);
        if (generation === requestRef.current) {
          setTotal(typeof data?.total === "number" ? data.total : null);
          setClosed(false);
        }
        return data?.users ?? [];
      } catch (err) {
        // Recorded before rethrowing: the pagination hook turns every failure
        // into the same sentence, and this one is not a failure at all.
        if (generation === requestRef.current && (err as ApiError)?.status === 403) {
          setClosed(true);
        }
        throw err;
      }
    },
    [query],
  );

  const { items, loading, hasMore, error, loadMore, retry, setItems } = useOffsetPagination<User>(
    fetcher,
    APPROVALS_LOAD_ERROR,
    applicantKey,
    APPROVALS_PAGE_SIZE,
  );

  const removeApproved = useCallback(
    (userId: string) => {
      setItems((applicants) => applicants.filter((a) => a.id !== userId));
      setTotal((current) => (current === null ? null : Math.max(0, current - 1)));
    },
    [setItems],
  );

  return {
    applicants: items,
    total,
    loading,
    hasMore: hasMore && (total === null || items.length < total),
    error: closed ? null : error,
    closed,
    loadMore,
    retry,
    removeApproved,
  };
}
