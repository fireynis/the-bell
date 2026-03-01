import { Link } from "react-router";
import AuthLayout from "../../components/AuthLayout.tsx";
import FlowForm from "../../components/FlowForm.tsx";
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
        <Link to="/auth/login" className="text-indigo-600 hover:text-indigo-500">
          Back to sign in
        </Link>
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
