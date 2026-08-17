import { Link, useLocation, useNavigate } from "react-router";
import AuthLayout from "../../components/AuthLayout.tsx";
import ErrorBanner from "../../components/ErrorBanner.tsx";
import FlowForm from "../../components/FlowForm.tsx";
import Spinner from "../../components/Spinner.tsx";
import { useAuth } from "../../context/AuthContext.tsx";
import { useTheme } from "../../context/ThemeContext.tsx";
import { useFlow } from "../../hooks/useFlow.ts";
import { LOGIN_INVITE_LINK, LOGIN_INVITE_ONLY, registrationMode } from "../../lib/invite.ts";

export default function Login() {
  const { flow, error, submitting, submit } = useFlow("login");
  const { refreshSession } = useAuth();
  const { config } = useTheme();
  const navigate = useNavigate();
  const location = useLocation();

  // "Register" promises a form. In a town that admits people by invitation
  // there is no form behind that link, so the link says what is actually there
  // — the page explaining how somebody gets invited.
  const inviteOnly = registrationMode(config) === "invite";

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
          {inviteOnly ? (
            <span>
              {LOGIN_INVITE_ONLY}.{" "}
              <Link to="/auth/registration" style={{ color: "var(--color-primary)" }}>
                {LOGIN_INVITE_LINK}
              </Link>
            </span>
          ) : (
            <Link to="/auth/registration" style={{ color: "var(--color-primary)" }}>
              Don't have an account? Register
            </Link>
          )}
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
