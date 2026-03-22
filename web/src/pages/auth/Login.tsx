import { Link, useLocation, useNavigate } from "react-router";
import AuthLayout from "../../components/AuthLayout.tsx";
import ErrorBanner from "../../components/ErrorBanner.tsx";
import FlowForm from "../../components/FlowForm.tsx";
import Spinner from "../../components/Spinner.tsx";
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
          <Link to="/auth/registration" style={{ color: "var(--color-primary)" }}>
            Don't have an account? Register
          </Link>
          <br />
          <Link to="/auth/recovery" style={{ color: "var(--color-primary)" }}>
            Forgot your password?
          </Link>
        </>
      }
    >
      {error && (
        <div className="mb-4">
          <ErrorBanner message={error} />
        </div>
      )}
      {flow ? (
        <FlowForm flow={flow} onSubmit={handleSubmit} submitting={submitting} />
      ) : (
        <div className="flex justify-center py-8">
          <Spinner />
        </div>
      )}
    </AuthLayout>
  );
}
