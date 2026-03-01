import { useState } from "react";
import AuthLayout from "../../components/AuthLayout.tsx";
import FlowForm from "../../components/FlowForm.tsx";
import { useFlow } from "../../hooks/useFlow.ts";

export default function Settings() {
  const { flow, error, submitting, submit } = useFlow("settings");
  const [success, setSuccess] = useState(false);

  const handleSubmit = async (values: Record<string, unknown>) => {
    setSuccess(false);
    const result = await submit(values);
    if (result.success) {
      setSuccess(true);
    }
  };

  return (
    <AuthLayout title="Account settings" subtitle="Update your password and profile">
      {error && (
        <div className="mb-4 rounded-md bg-red-50 p-3 text-sm text-red-700">{error}</div>
      )}
      {success && (
        <div className="mb-4 rounded-md bg-green-50 p-3 text-sm text-green-700">
          Settings updated successfully.
        </div>
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
