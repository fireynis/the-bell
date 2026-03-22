import { useState } from "react";
import ErrorBanner from "../../components/ErrorBanner.tsx";
import FlowForm from "../../components/FlowForm.tsx";
import Spinner from "../../components/Spinner.tsx";
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
    <div className="py-5">
      <h1
        className="mb-5 text-xl font-bold"
        style={{ fontFamily: "var(--font-display)", color: "var(--color-text)" }}
      >
        Account Settings
      </h1>

      <div
        className="rounded-[var(--radius-lg)] p-6"
        style={{ backgroundColor: "var(--color-surface)", boxShadow: "var(--shadow-md)" }}
      >
        <p className="mb-4 text-sm" style={{ color: "var(--color-text-secondary)" }}>
          Update your password and profile
        </p>

        {error && <div className="mb-4"><ErrorBanner message={error} /></div>}

        {success && (
          <div
            className="mb-4 rounded-[var(--radius-md)] p-3 text-sm"
            style={{ backgroundColor: "var(--color-success-light)", color: "var(--color-success)" }}
          >
            Settings updated successfully.
          </div>
        )}

        {flow ? (
          <FlowForm flow={flow} onSubmit={handleSubmit} submitting={submitting} />
        ) : (
          <div className="flex justify-center py-8">
            <Spinner />
          </div>
        )}
      </div>
    </div>
  );
}
