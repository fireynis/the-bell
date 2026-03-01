import { Link, useNavigate } from "react-router";
import AuthLayout from "../../components/AuthLayout.tsx";
import FlowForm from "../../components/FlowForm.tsx";
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
        <Link to="/auth/login" className="text-indigo-600 hover:text-indigo-500">
          Already have an account? Sign in
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
