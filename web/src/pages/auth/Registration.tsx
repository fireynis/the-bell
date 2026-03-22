import { Link, useNavigate } from "react-router";
import AuthLayout from "../../components/AuthLayout.tsx";
import ErrorBanner from "../../components/ErrorBanner.tsx";
import FlowForm from "../../components/FlowForm.tsx";
import Spinner from "../../components/Spinner.tsx";
import { useAuth } from "../../context/AuthContext.tsx";
import { useFlow } from "../../hooks/useFlow.ts";

export default function Registration() {
  const { flow, error, submitting, submit } = useFlow("registration");
  const { refreshSession } = useAuth();
  const navigate = useNavigate();

  const handleSubmit = async (values: Record<string, unknown>) => {
    const result = await submit(values);
    if (result.success) {
      await refreshSession();
      navigate("/", { replace: true });
    }
  };

  return (
    <AuthLayout
      title="Create an account"
      subtitle="Join The Bell community"
      footer={
        <Link to="/auth/login" style={{ color: "var(--color-primary)" }}>
          Already have an account? Sign in
        </Link>
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
