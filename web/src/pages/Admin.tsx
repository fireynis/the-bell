import { useCallback, useEffect, useState } from "react";
import { api, proposalApi } from "../api/client";
import { useAuth } from "../context/AuthContext";
import Spinner from "../components/Spinner";
import ErrorBanner from "../components/ErrorBanner";
import ThemeSettings from "./admin/ThemeSettings";
import PendingUsersSection from "./admin/PendingUsersSection";
import ProposalsSection from "./admin/ProposalsSection";
import { isCouncil } from "../lib/trust";
import { applyProposalUpdate, proposalErrorMessage } from "../lib/proposal";
import { NAV_ITEMS } from "../lib/nav";
import type {
  ApiError,
  TownStats,
  Proposal,
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

export default function Admin() {
  const { user } = useAuth();

  const [stats, setStats] = useState<TownStats | null>(null);
  const [pendingUsers, setPendingUsers] = useState<User[]>([]);
  const [proposals, setProposals] = useState<Proposal[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [approving, setApproving] = useState<string | null>(null);
  const [voting, setVoting] = useState<string | null>(null);

  const council = isCouncil(user);

  const fetchData = useCallback(async () => {
    setLoading(true);
    setError(null);

    try {
      // Open and decided are separate reads — the history grows without bound
      // and has no business being downloaded to find three open votes — but
      // they are held as one list here, so that a vote which decides a proposal
      // simply moves it from one half of the town hall to the other.
      const [statsData, pendingResult, open, decided] = await Promise.all([
        api.get<TownStats>("/admin/stats"),
        api.get<PendingUsersResponse>("/vouches/pending").catch(() => null),
        proposalApi.list("open"),
        proposalApi.list("decided"),
      ]);

      setStats(statsData);
      setPendingUsers(pendingResult?.users ?? []);
      setProposals([...(open.proposals ?? []), ...(decided.proposals ?? [])]);
    } catch (err) {
      const apiErr = err as ApiError;
      setError(apiErr.error ?? `Failed to load ${NAV_ITEMS.admin.label}.`);
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
    setError(null);
    try {
      // The response is the whole updated proposal, and this vote may have been
      // the one that decided it — in which case a promotion or a removal has
      // already been carried out. Folding the server's copy back in is what puts
      // that on screen without a reload.
      const updated = await proposalApi.vote(proposalId, vote);
      setProposals((prev) => applyProposalUpdate(prev, updated));
    } catch (err) {
      setError(proposalErrorMessage(err as ApiError, "vote"));
    } finally {
      setVoting(null);
    }
  }

  // These pages carry no container of their own: the route mounts them inside
  // AppLayout's wide variant, which centres them and supplies the gutter.
  if (!council) {
    return (
      <div className="py-5">
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
      <div className="py-5">
        <div className="flex justify-center py-12">
          <Spinner size="lg" />
        </div>
      </div>
    );
  }

  return (
    <div className="py-5">
      {/* Named for the destination the town navigates to, not for the role
          that reaches it — the nav says Town Hall, so the page does too. */}
      <h1
        className="mb-6 text-2xl font-bold"
        style={{
          color: "var(--color-text)",
          fontFamily: "var(--font-display)",
        }}
      >
        {NAV_ITEMS.admin.label}
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

        <ProposalsSection
          proposals={proposals}
          viewerId={user?.id ?? null}
          onVote={handleVote}
          voting={voting}
          onCreated={fetchData}
        />

        <ThemeSettings />
      </div>
    </div>
  );
}
