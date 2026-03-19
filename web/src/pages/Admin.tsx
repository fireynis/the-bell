import { useCallback, useEffect, useState } from "react";
import { Link } from "react-router";
import { api } from "../api/client";
import { useAuth } from "../context/AuthContext";
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
  accent,
}: {
  label: string;
  value: number;
  accent?: string;
}) {
  const colorClass = accent ?? "text-indigo-600";
  return (
    <div className="rounded-lg bg-white p-5 shadow">
      <p className="text-sm font-medium text-gray-500">{label}</p>
      <p className={`mt-1 text-3xl font-bold ${colorClass}`}>{value}</p>
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
        accent="text-green-600"
      />
      <StatCard
        label="Active Moderators"
        value={stats.active_moderators}
        accent="text-blue-600"
      />
      <StatCard
        label="Pending Users"
        value={stats.pending_users}
        accent={stats.pending_users > 0 ? "text-amber-600" : "text-gray-400"}
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
      <div className="rounded-lg bg-white p-6 shadow">
        <h2 className="text-lg font-semibold text-gray-900">
          Pending User Approvals
        </h2>
        <p className="mt-2 text-sm text-gray-500">
          No pending users at this time.
        </p>
      </div>
    );
  }

  return (
    <div className="rounded-lg bg-white p-6 shadow">
      <h2 className="mb-4 text-lg font-semibold text-gray-900">
        Pending User Approvals
      </h2>
      <ul className="divide-y divide-gray-100">
        {users.map((user) => (
          <li key={user.id} className="flex items-center justify-between py-3">
            <div>
              <Link
                to={`/profile/${user.id}`}
                className="text-sm font-medium text-indigo-600 hover:text-indigo-500"
              >
                {user.display_name || user.id.slice(0, 8)}
              </Link>
              <p className="text-xs text-gray-500">
                Joined {new Date(user.joined_at).toLocaleDateString()}
              </p>
            </div>
            <button
              onClick={() => onApprove(user.id)}
              disabled={approving === user.id}
              className="rounded-md bg-green-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-green-500 disabled:cursor-not-allowed disabled:opacity-50"
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
      <div className="rounded-lg bg-white p-6 shadow">
        <h2 className="text-lg font-semibold text-gray-900">
          Council Proposals
        </h2>
        <p className="mt-2 text-sm text-gray-500">
          No open proposals at this time.
        </p>
      </div>
    );
  }

  return (
    <div className="rounded-lg bg-white p-6 shadow">
      <h2 className="mb-4 text-lg font-semibold text-gray-900">
        Council Proposals
      </h2>
      <ul className="space-y-4">
        {proposals.map((proposal) => (
          <li
            key={proposal.proposal_id}
            className="rounded-md border border-gray-200 p-4"
          >
            <div className="flex items-start justify-between">
              <div>
                <p className="text-sm font-medium text-gray-900">
                  Proposal {proposal.proposal_id.slice(0, 8)}
                </p>
                <div className="mt-1 flex gap-4 text-xs text-gray-500">
                  <span className="text-green-600">
                    {proposal.approve_count} approve
                  </span>
                  <span className="text-red-600">
                    {proposal.reject_count} reject
                  </span>
                  <span>
                    {proposal.approve_count + proposal.reject_count} /{" "}
                    {proposal.total_council} voted
                  </span>
                </div>
                <div className="mt-2">
                  <div className="h-2 w-48 rounded-full bg-gray-200">
                    <div
                      className="h-2 rounded-full bg-green-500"
                      style={{
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
                  className="rounded-md bg-green-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-green-500 disabled:cursor-not-allowed disabled:opacity-50"
                >
                  Approve
                </button>
                <button
                  onClick={() => onVote(proposal.proposal_id, "reject")}
                  disabled={voting === proposal.proposal_id}
                  className="rounded-md bg-red-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-red-500 disabled:cursor-not-allowed disabled:opacity-50"
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
        prev.map((p) =>
          p.proposal_id === proposalId ? updated : p,
        ).filter((p) => p.status === "pending"),
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
        <div className="rounded-md bg-red-50 p-4 text-sm text-red-700">
          You do not have permission to view this page.
        </div>
        <Link
          to="/"
          className="mt-4 inline-block text-sm text-indigo-600 hover:text-indigo-500"
        >
          Back to feed
        </Link>
      </div>
    );
  }

  if (loading) {
    return (
      <div className="mx-auto max-w-4xl p-4">
        <div className="flex justify-center py-12">
          <div className="h-8 w-8 animate-spin rounded-full border-2 border-gray-300 border-t-indigo-600" />
        </div>
      </div>
    );
  }

  return (
    <div className="mx-auto max-w-4xl p-4">
      <div className="mb-6 flex items-center justify-between">
        <h1 className="text-2xl font-bold text-gray-900">Admin Dashboard</h1>
        <Link to="/" className="text-sm text-indigo-600 hover:text-indigo-500">
          Back to feed
        </Link>
      </div>

      {error && (
        <div className="mb-4 rounded-md bg-red-50 p-3 text-sm text-red-700">
          {error}
          <button onClick={fetchData} className="ml-2 font-medium underline">
            Retry
          </button>
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
      </div>
    </div>
  );
}
