import { Navigate, Outlet } from "react-router";
import { useAuth } from "../context/AuthContext.tsx";
import Spinner from "./Spinner";

const ROLE_RANK: Record<string, number> = {
  banned: 0,
  pending: 1,
  member: 2,
  moderator: 3,
  council: 4,
};

interface RequireRoleProps {
  minRole: string;
}

export default function RequireRole({ minRole }: RequireRoleProps) {
  const { user, loading } = useAuth();

  if (loading) {
    return (
      <div className="flex min-h-screen items-center justify-center">
        <Spinner size="lg" />
      </div>
    );
  }

  const userRank = ROLE_RANK[user?.role ?? ""] ?? 0;
  const requiredRank = ROLE_RANK[minRole] ?? 0;

  if (!user || userRank < requiredRank) {
    return <Navigate to="/" replace />;
  }

  return <Outlet />;
}
