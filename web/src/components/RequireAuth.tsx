import { Navigate, Outlet, useLocation } from "react-router";
import { useAuth } from "../context/AuthContext.tsx";
import Spinner from "./Spinner";

export default function RequireAuth() {
  const { session, loading } = useAuth();
  const location = useLocation();

  if (loading) {
    return (
      <div className="flex min-h-screen items-center justify-center">
        <Spinner size="lg" />
      </div>
    );
  }

  if (!session) {
    return <Navigate to="/auth/login" state={{ from: location }} replace />;
  }

  return <Outlet />;
}
