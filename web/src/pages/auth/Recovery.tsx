import { Link } from "react-router";
import AuthLayout from "../../components/AuthLayout.tsx";
import ErrorBanner from "../../components/ErrorBanner.tsx";
import FlowForm from "../../components/FlowForm.tsx";
import Spinner from "../../components/Spinner.tsx";
import { useFlow } from "../../hooks/useFlow.ts";

export default function Recovery() {
  const { flow, error, submitting, submit } = useFlow("recovery");

  const handleSubmit = async (values: Record<string, unknown>) => {
    await submit(values);
    // Kratos handles the multi-step state machine (email → code).
    // After submission, the flow is re-fetched with the next step's UI nodes.
  };

  return (
    <AuthLayout
      title="Account recovery"
      subtitle="Enter your email to receive a recovery code"
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
