import { Link, useLocation, useNavigate } from "react-router";
import AuthLayout from "../../components/AuthLayout.tsx";
import FlowForm from "../../components/FlowForm.tsx";
import { useAuth } from "../../context/AuthContext.tsx";
import { useFlow } from "../../hooks/useFlow.ts";

export default function Login() {
  const { flow, error, submitting, submit } = useFlow("login");
  const { refreshSession } = useAuth();
  const navigate = useNavigate();
  const location = useLocation();

  const returnTo = (location.state as { from?: { pathname: string } })?.from?.pathname ?? "/";

  const handleSubmit = async (values: Record<string, unknown>) => {
    const result = await submit(values);
    if (result.success) {
      await refreshSession();
      navigate(returnTo, { replace: true });
    }
  };

  return (
    <AuthLayout
      title="Sign in"
      subtitle="Welcome back to The Bell"
      footer={
        <>
          <Link to="/auth/registration" className="text-indigo-600 hover:text-indigo-500">
            Don't have an account? Register
          </Link>
          <br />
          <Link to="/auth/recovery" className="text-indigo-600 hover:text-indigo-500">
            Forgot your password?
          </Link>
        </>
      }
    >
      {error && (
        <div className="mb-4 rounded-md bg-red-50 p-3 text-sm text-red-700">{error}</div>
      )}
      {flow ? (
        <FlowForm flow={flow} onSubmit={handleSubmit} submitting={submitting} />
      ) : (
        <div className="flex justify-center py-8">
          <div className="h-6 w-6 animate-spin rounded-full border-2 border-indigo-600 border-t-transparent" />
        </div>
      )}
    </AuthLayout>
  );
}
