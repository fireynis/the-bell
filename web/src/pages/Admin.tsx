import { useCallback, useEffect, useState } from "react";
import { Link } from "react-router";
import { api } from "../api/client";
import { useAuth } from "../context/AuthContext";
import Spinner from "../components/Spinner";
import ErrorBanner from "../components/ErrorBanner";
import ThemeSettings from "./admin/ThemeSettings";
import type {
  ApiError,
  TownStats,
  ProposalSummary,
  ProposalsResponse,
  PendingUsersResponse,
  User,
} from "../api/types";

function StatCard({
  label,
  value,
  accentVar,
}: {
  label: string;
  value: number;
  accentVar?: string;
}) {
  return (
    <div
      className="p-5"
      style={{
        backgroundColor: "var(--color-surface)",
        boxShadow: "var(--shadow-sm)",
        borderRadius: "var(--radius-lg)",
      }}
    >
      <p className="text-sm font-medium" style={{ color: "var(--color-text-secondary)" }}>
        {label}
      </p>
      <p
        className="mt-1 text-3xl font-bold"
        style={{ color: accentVar ?? "var(--color-primary)" }}
      >
        {value}
      </p>
    </div>
  );
}

function StatsPanel({ stats }: { stats: TownStats }) {
  return (
    <div className="grid grid-cols-2 gap-4 sm:grid-cols-4">
      <StatCard label="Total Users" value={stats.total_users} />
      <StatCard
        label="Posts Today"
        value={stats.posts_today}
        accentVar="var(--color-success)"
      />
      <StatCard
        label="Active Moderators"
        value={stats.active_moderators}
        accentVar="var(--color-info)"
      />
      <StatCard
        label="Pending Users"
        value={stats.pending_users}
        accentVar={
          stats.pending_users > 0
            ? "var(--color-warning)"
            : "var(--color-text-tertiary)"
        }
      />
    </div>
  );
}

function PendingUsersSection({
  users,
  onApprove,
  approving,
}: {
  users: User[];
  onApprove: (id: string) => void;
  approving: string | null;
}) {
  if (users.length === 0) {
    return (
      <div
        className="p-6"
        style={{
          backgroundColor: "var(--color-surface)",
          boxShadow: "var(--shadow-sm)",
          borderRadius: "var(--radius-lg)",
        }}
      >
        <h2 className="text-lg font-semibold" style={{ color: "var(--color-text)" }}>
          Pending User Approvals
        </h2>
        <p className="mt-2 text-sm" style={{ color: "var(--color-text-secondary)" }}>
          No pending users at this time.
        </p>
      </div>
    );
  }

  return (
    <div
      className="p-6"
      style={{
        backgroundColor: "var(--color-surface)",
        boxShadow: "var(--shadow-sm)",
        borderRadius: "var(--radius-lg)",
      }}
    >
      <h2 className="mb-4 text-lg font-semibold" style={{ color: "var(--color-text)" }}>
        Pending User Approvals
      </h2>
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
    </div>
  );
}

function CouncilVotesSection({
  proposals,
  onVote,
  voting,
}: {
  proposals: ProposalSummary[];
  onVote: (proposalId: string, vote: "approve" | "reject") => void;
  voting: string | null;
}) {
  if (proposals.length === 0) {
    return (
      <div
        className="p-6"
        style={{
          backgroundColor: "var(--color-surface)",
          boxShadow: "var(--shadow-sm)",
          borderRadius: "var(--radius-lg)",
        }}
      >
        <h2 className="text-lg font-semibold" style={{ color: "var(--color-text)" }}>
          Council Proposals
        </h2>
        <p className="mt-2 text-sm" style={{ color: "var(--color-text-secondary)" }}>
          No open proposals at this time.
        </p>
      </div>
    );
  }

  return (
    <div
      className="p-6"
      style={{
        backgroundColor: "var(--color-surface)",
        boxShadow: "var(--shadow-sm)",
        borderRadius: "var(--radius-lg)",
      }}
    >
      <h2 className="mb-4 text-lg font-semibold" style={{ color: "var(--color-text)" }}>
        Council Proposals
      </h2>
      <ul className="space-y-4">
        {proposals.map((proposal) => (
          <li
            key={proposal.proposal_id}
            className="p-4"
            style={{
              borderWidth: "1px",
              borderStyle: "solid",
              borderColor: "var(--color-border)",
              borderRadius: "var(--radius-md)",
            }}
          >
            <div className="flex items-start justify-between">
              <div>
                <p className="text-sm font-medium" style={{ color: "var(--color-text)" }}>
                  Proposal {proposal.proposal_id.slice(0, 8)}
                </p>
                <div className="mt-1 flex gap-4 text-xs" style={{ color: "var(--color-text-secondary)" }}>
                  <span style={{ color: "var(--color-success)" }}>
                    {proposal.approve_count} approve
                  </span>
                  <span style={{ color: "var(--color-danger)" }}>
                    {proposal.reject_count} reject
                  </span>
                  <span>
                    {proposal.approve_count + proposal.reject_count} /{" "}
                    {proposal.total_council} voted
                  </span>
                </div>
                <div className="mt-2">
                  <div
                    className="h-2 w-48 rounded-full"
                    style={{ backgroundColor: "var(--color-border)" }}
                  >
                    <div
                      className="h-2 rounded-full"
                      style={{
                        backgroundColor: "var(--color-success)",
                        width: `${
                          proposal.total_council > 0
                            ? (proposal.approve_count /
                                proposal.total_council) *
                              100
                            : 0
                        }%`,
                      }}
                    />
                  </div>
                </div>
              </div>
              <div className="flex gap-2">
                <button
                  onClick={() => onVote(proposal.proposal_id, "approve")}
                  disabled={voting === proposal.proposal_id}
                  className="rounded-md px-3 py-1.5 text-sm font-medium disabled:cursor-not-allowed disabled:opacity-50"
                  style={{
                    backgroundColor: "var(--color-success)",
                    color: "var(--color-text-inverse)",
                  }}
                >
                  Approve
                </button>
                <button
                  onClick={() => onVote(proposal.proposal_id, "reject")}
                  disabled={voting === proposal.proposal_id}
                  className="rounded-md px-3 py-1.5 text-sm font-medium disabled:cursor-not-allowed disabled:opacity-50"
                  style={{
                    backgroundColor: "var(--color-danger)",
                    color: "var(--color-text-inverse)",
                  }}
                >
                  Reject
                </button>
              </div>
            </div>
          </li>
        ))}
      </ul>
    </div>
  );
}

export default function Admin() {
  const { user } = useAuth();

  const [stats, setStats] = useState<TownStats | null>(null);
  const [pendingUsers, setPendingUsers] = useState<User[]>([]);
  const [proposals, setProposals] = useState<ProposalSummary[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [approving, setApproving] = useState<string | null>(null);
  const [voting, setVoting] = useState<string | null>(null);

  const isCouncil = user?.role === "council";

  const fetchData = useCallback(async () => {
    setLoading(true);
    setError(null);

    try {
      const [statsData, pendingResult, proposalsData] = await Promise.all([
        api.get<TownStats>("/admin/stats"),
        api.get<PendingUsersResponse>("/vouches/pending").catch(() => null),
        api.get<ProposalsResponse>("/admin/council/votes"),
      ]);

      setStats(statsData);
      setPendingUsers(pendingResult?.users ?? []);
      setProposals(proposalsData.proposals ?? []);
    } catch (err) {
      const apiErr = err as ApiError;
      setError(apiErr.error ?? "Failed to load admin dashboard.");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchData();
  }, [fetchData]);

  async function handleApprove(userId: string) {
    setApproving(userId);
    try {
      await api.post(`/vouches/approve/${userId}`, {});
      setPendingUsers((prev) => prev.filter((u) => u.id !== userId));
      // Refresh stats to update pending count
      try {
        const updatedStats = await api.get<TownStats>("/admin/stats");
        setStats(updatedStats);
      } catch {
        // Non-critical — stats will refresh on next load
      }
    } catch (err) {
      const apiErr = err as ApiError;
      setError(apiErr.error ?? "Failed to approve user.");
    } finally {
      setApproving(null);
    }
  }

  async function handleVote(proposalId: string, vote: "approve" | "reject") {
    setVoting(proposalId);
    try {
      const updated = await api.post<ProposalSummary>(
        "/admin/council/votes",
        { proposal_id: proposalId, vote },
      );
      setProposals((prev) =>
        prev
          .map((p) => (p.proposal_id === proposalId ? updated : p))
          .filter((p) => p.status === "pending"),
      );
    } catch (err) {
      const apiErr = err as ApiError;
      setError(apiErr.error ?? "Failed to cast vote.");
    } finally {
      setVoting(null);
    }
  }

  if (!isCouncil) {
    return (
      <div className="mx-auto max-w-4xl p-4">
        <div
          className="rounded-[var(--radius-md)] p-4 text-sm"
          style={{
            backgroundColor: "var(--color-danger-light)",
            color: "var(--color-danger)",
          }}
        >
          You do not have permission to view this page.
        </div>
      </div>
    );
  }

  if (loading) {
    return (
      <div className="mx-auto max-w-4xl p-4">
        <div className="flex justify-center py-12">
          <Spinner size="lg" />
        </div>
      </div>
    );
  }

  return (
    <div className="mx-auto max-w-4xl p-4">
      <div className="py-5">
        <h1
          className="mb-6 text-2xl font-bold"
          style={{
            color: "var(--color-text)",
            fontFamily: "var(--font-display)",
          }}
        >
          Admin Dashboard
        </h1>

        {error && (
          <div className="mb-4">
            <ErrorBanner message={error} onRetry={fetchData} />
          </div>
        )}

        <div className="space-y-6">
          {stats && <StatsPanel stats={stats} />}

          <PendingUsersSection
            users={pendingUsers}
            onApprove={handleApprove}
            approving={approving}
          />

          <CouncilVotesSection
            proposals={proposals}
            onVote={handleVote}
            voting={voting}
          />

          <ThemeSettings />
        </div>
      </div>
    </div>
  );
}
