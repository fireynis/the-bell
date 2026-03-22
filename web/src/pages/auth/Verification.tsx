import { Link } from "react-router";
import AuthLayout from "../../components/AuthLayout.tsx";
import ErrorBanner from "../../components/ErrorBanner.tsx";
import FlowForm from "../../components/FlowForm.tsx";
import Spinner from "../../components/Spinner.tsx";
import { useFlow } from "../../hooks/useFlow.ts";

export default function Verification() {
  const { flow, error, submitting, submit } = useFlow("verification");

  const handleSubmit = async (values: Record<string, unknown>) => {
    await submit(values);
    // Kratos handles the multi-step state machine (email → code).
  };

  return (
    <AuthLayout
      title="Email verification"
      subtitle="Verify your email address"
      footer={
        <Link to="/auth/login" style={{ color: "var(--color-primary)" }}>
          Back to sign in
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
