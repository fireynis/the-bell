import { useRef, useState } from "react";
import { useNavigate } from "react-router";
import ErrorBanner from "../../components/ErrorBanner.tsx";
import FlowForm from "../../components/FlowForm.tsx";
import Spinner from "../../components/Spinner.tsx";
import { useAuth } from "../../context/AuthContext.tsx";
import { useFlow } from "../../hooks/useFlow.ts";

export default function Settings() {
  const { flow, error, submitting, submit } = useFlow("settings");
  const { logout } = useAuth();
  const navigate = useNavigate();
  const [success, setSuccess] = useState(false);
  const [signingOut, setSigningOut] = useState(false);
  // A ref as well as the state: the state is what disables the button, but it
  // only takes effect on the next render, so two taps inside one tick would
  // both get past a state-only guard and start two logout flows.
  const signingOutRef = useRef(false);

  const handleSubmit = async (values: Record<string, unknown>) => {
    setSuccess(false);
    const result = await submit(values);
    if (result.success) {
      setSuccess(true);
    }
  };

  /**
   * The same logout the sidebar pill runs — AuthContext's, which ends the
   * Kratos session and clears the local one. The navigate is this page's own
   * addition: the sidebar sits inside the app shell, which re-renders itself
   * out of an authenticated state, while this page would otherwise stay on
   * screen looking signed in.
   */
  const handleSignOut = async () => {
    if (signingOutRef.current) return;
    signingOutRef.current = true;
    setSigningOut(true);
    await logout();
    navigate("/auth/login", { replace: true });
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

      {/*
        The only way off the app on a phone. Signing out lived in the desktop
        sidebar pill, which is `hidden lg:flex`, so below lg there was no way to
        leave a session at all — and this page is where somebody looking for one
        goes. It is deliberately its own card, well clear of the password form:
        the two are not the same kind of act.
      */}
      <div
        className="mt-5 flex items-center justify-between gap-4 rounded-[var(--radius-lg)] p-6"
        style={{ backgroundColor: "var(--color-surface)", boxShadow: "var(--shadow-md)" }}
      >
        <div>
          <h2 className="text-sm font-semibold" style={{ color: "var(--color-text)" }}>
            Sign out
          </h2>
          <p className="mt-1 text-sm" style={{ color: "var(--color-text-secondary)" }}>
            Ends this session on this device.
          </p>
        </div>
        <button
          type="button"
          onClick={handleSignOut}
          disabled={signingOut}
          className="btn btn-quiet flex-shrink-0 rounded-[var(--radius-md)] px-4 py-2 text-sm font-medium disabled:opacity-50"
        >
          {signingOut ? "Signing out…" : "Sign out"}
        </button>
      </div>
    </div>
  );
}
