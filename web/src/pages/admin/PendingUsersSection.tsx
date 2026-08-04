import { Link } from "react-router";
import type { User } from "../../api/types";
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
        {users.map((user) => (
          <li
            key={user.id}
            className="flex items-center justify-between py-3"
            style={{ borderBottom: "1px solid var(--color-border-light)" }}
          >
            <div>
              <Link
                to={`/profile/${user.id}`}
                className="text-sm font-medium"
                style={{ color: "var(--color-primary)" }}
              >
                {user.display_name || user.id.slice(0, 8)}
              </Link>
              <p className="text-xs" style={{ color: "var(--color-text-secondary)" }}>
                Joined {new Date(user.joined_at).toLocaleDateString()}
              </p>
            </div>
            <button
              onClick={() => onApprove(user.id)}
              disabled={approving === user.id}
              className="rounded-md px-3 py-1.5 text-sm font-medium disabled:cursor-not-allowed disabled:opacity-50"
              style={{
                backgroundColor: "var(--color-success)",
                color: "var(--color-text-inverse)",
              }}
            >
              {approving === user.id ? "Approving..." : "Approve"}
            </button>
          </li>
        ))}
      </ul>
    </AdminSection>
  );
}
