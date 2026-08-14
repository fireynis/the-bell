import { Link } from "react-router";
import type { User } from "../../api/types";
import { personName } from "../../lib/people";
import { NO_RESIDENCY_CLAIM, describeResidencyClaim } from "../../lib/residency";
import AdminSection from "./AdminSection";

/** PendingUsersSection lists users awaiting council approval, newest first. */
export default function PendingUsersSection({
  users,
  onApprove,
  approving,
}: {
  users: User[];
  onApprove: (id: string) => void;
  approving: string | null;
}) {
  return (
    <AdminSection
      title="Pending User Approvals"
      isEmpty={users.length === 0}
      emptyMessage="No pending users at this time."
    >
      <ul style={{ borderTop: "1px solid var(--color-border-light)" }}>
        {users.map((user) => {
          const said = describeResidencyClaim(user.residency_claim);

          return (
          <li
            key={user.id}
            className="flex items-center justify-between gap-4 py-3"
            style={{ borderBottom: "1px solid var(--color-border-light)" }}
          >
            <div className="min-w-0">
              <Link
                to={`/profile/${user.id}`}
                className="text-sm font-medium"
                style={{ color: "var(--color-primary)" }}
              >
                {/* Most pending users arrive with a name now that registration
                    syncs the trait, but the fallback stays: a council member
                    still has to be able to approve one who set none. */}
                {personName(user.display_name, user.id)}
              </Link>
              <p className="text-xs" style={{ color: "var(--color-text-secondary)" }}>
                Joined {new Date(user.joined_at).toLocaleDateString()}
              </p>
              {/*
                Rendered as something this person said, never as an address the
                town holds — see describeResidencyClaim. This queue is the only
                place in the app it appears; it is deliberately absent from
                profiles and from the directory, and the server enforces that
                too. Somebody who gave none gets a quiet line rather than a gap,
                so a council member can tell "did not say" from "did not load".
              */}
              <p
                className="mt-0.5 truncate text-xs italic"
                style={{ color: said ? "var(--color-text-secondary)" : "var(--color-text-tertiary)" }}
                title={said ?? undefined}
              >
                {said ?? NO_RESIDENCY_CLAIM}
              </p>
            </div>
            <button
              onClick={() => onApprove(user.id)}
              disabled={approving === user.id}
              className="flex-shrink-0 rounded-md px-3 py-1.5 text-sm font-medium disabled:cursor-not-allowed disabled:opacity-50"
              style={{
                backgroundColor: "var(--color-success)",
                color: "var(--color-text-inverse)",
              }}
            >
              {approving === user.id ? "Approving..." : "Approve"}
            </button>
          </li>
          );
        })}
      </ul>
    </AdminSection>
  );
}
