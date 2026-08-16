import { useRef } from "react";
import { useParams } from "react-router";
import { useAuth } from "../../context/AuthContext.tsx";
import { useActionHistory } from "../../hooks/useActionHistory.ts";
import { useIntersectionObserver } from "../../hooks/useIntersectionObserver.ts";
import ActionHistoryCard from "../../components/ActionHistoryCard.tsx";
import { MuteBanner, SuspensionBanner } from "../../components/RestrictionBanner.tsx";
import ErrorBanner from "../../components/ErrorBanner.tsx";
import Spinner from "../../components/Spinner.tsx";
import { describeHistorySubject } from "../../lib/moderation.ts";

export default function UserHistory() {
  const { id } = useParams<{ id: string }>();
  const { user } = useAuth();
  const { entries, loading, hasMore, error, loadMore, retry } =
    useActionHistory(id!);
  const sentinelRef = useRef<HTMLDivElement>(null);

  useIntersectionObserver(sentinelRef, loadMore, hasMore && !loading);

  // No inner container: AppLayout already centres the column and supplies
  // the horizontal gutter.
  return (
    <div className="py-5">
      <div className="mb-6">
        <h1
          className="text-2xl font-bold"
          style={{ fontFamily: "var(--font-display)", color: "var(--color-text)" }}
        >
          User Action History
        </h1>
        {/* Named from the actions themselves — every one of them is addressed
            to this member — with the full id on hover, since the page has no
            other read that would tell it who this is. */}
        <p className="text-sm" title={id} style={{ color: "var(--color-text-tertiary)" }}>
          User: {describeHistorySubject(entries, id ?? "")}
        </p>
      </div>

      {/* What is in force right now, which the history below cannot answer: a
          mute or suspend action stays in the trail unchanged after the
          restriction is lifted or lapses, keeping its original expiry forever.
          This is also the only place either can be ended early. Both banners
          render nothing when there is nothing in force, so a member under
          neither restriction sees no gap here. */}
      <MuteBanner userId={id!} viewerId={user?.id ?? ""} />
      <SuspensionBanner userId={id!} viewerId={user?.id ?? ""} />

      {error && (
        <div className="mb-4">
          <ErrorBanner message={error} onRetry={retry} />
        </div>
      )}

      {entries.length === 0 && !loading && !error && (
        <p style={{ color: "var(--color-text-tertiary)" }}>
          No moderation actions for this user.
        </p>
      )}

      <div className="flex flex-col gap-4">
        {entries.map((entry) => (
          <ActionHistoryCard key={entry.action.id} entry={entry} />
        ))}
      </div>

      {loading && (
        <div className="flex justify-center py-6">
          <Spinner />
        </div>
      )}

      <div ref={sentinelRef} className="h-1" />
    </div>
  );
}
