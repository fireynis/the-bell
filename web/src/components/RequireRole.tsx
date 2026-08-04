import { Navigate, Outlet } from "react-router";
import { useAuth } from "../context/AuthContext.tsx";
import Spinner from "./Spinner";
import { hasMinRole, type Role } from "../lib/trust.ts";

interface RequireRoleProps {
  minRole: Role;
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

  if (!hasMinRole(user, minRole)) {
    return <Navigate to="/" replace />;
  }

  return <Outlet />;
}
