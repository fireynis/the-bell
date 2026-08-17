import { useState } from "react";
import { Link } from "react-router";
import type { User } from "../../api/types";
import {
  APPROVALS_EMPTY,
  APPROVALS_PREVIEW_SIZE,
  describeQueueSize,
  describeWaitingTime,
} from "../../lib/approvals";
import { personName } from "../../lib/people";
import { formatDate } from "../../lib/time";
import AdminSection from "./AdminSection";

/**
 * PendingUsersSection previews the approval queue on the Town Hall dashboard.
 *
 * It used to be the queue: every pending user, with an Approve button each.
 * Fifty applicants — a town launch, or a registration flood — turned the
 * dashboard into an unusable wall, and a dashboard is for knowing where things
 * stand rather than for working through a list. The queue lives at
 * /admin/approvals now; what stays here is how many are waiting, who has waited
 * longest, and the way in.
 *
 * There is no Approve button, on purpose. Approving somebody is a judgement
 * about a stranger and it wants the queue's context — the residency claim, the
 * link to their profile, the rest of the list. A one-click approve sitting in a
 * grid of statistics invites the opposite.
 *
 * The residency claim is deliberately absent too. It is the most sensitive
 * thing a resident tells the town and it appears on the reviewing screen alone;
 * a preview on a dashboard is not a review.
 */
export default function PendingUsersSection({ users, total }: { users: User[]; total: number }) {
  // Read once, when the dashboard loads. The waits are counted in hours and
  // days, so a clock re-read on every render would buy nothing and make the
  // render impure; the figure refreshes when the page does.
  const [now] = useState(() => Date.now());

  const waiting = describeQueueSize(total);
  const previewed = users.slice(0, APPROVALS_PREVIEW_SIZE);

  return (
    <AdminSection
      title="Approvals"
      isEmpty={total <= 0}
      emptyMessage={APPROVALS_EMPTY}
      action={
        <Link
          to="/admin/approvals"
          className="text-sm font-medium"
          style={{ color: "var(--color-primary)" }}
        >
          Review all
        </Link>
      }
    >
      <p className="text-sm font-medium" style={{ color: "var(--color-text)" }}>
        {waiting}
      </p>

      <ul className="mt-3" style={{ borderTop: "1px solid var(--color-border-light)" }}>
        {previewed.map((user) => {
          const joined = formatDate(user.joined_at);
          const meta = [joined && `Joined ${joined}`, describeWaitingTime(user.joined_at, now)]
            .filter(Boolean)
            .join(" · ");

          return (
            <li
              key={user.id}
              className="py-2"
              style={{ borderBottom: "1px solid var(--color-border-light)" }}
            >
              <Link
                to={`/profile/${user.id}`}
                className="text-sm font-medium"
                style={{ color: "var(--color-primary)" }}
              >
                {personName(user.display_name, user.id)}
              </Link>
              {meta && (
                <p className="text-xs" style={{ color: "var(--color-text-secondary)" }}>
                  {meta}
                </p>
              )}
            </li>
          );
        })}
      </ul>
    </AdminSection>
  );
}
