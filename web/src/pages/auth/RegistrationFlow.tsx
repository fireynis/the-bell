import { useNavigate } from "react-router";
import ErrorBanner from "../../components/ErrorBanner.tsx";
import FlowForm from "../../components/FlowForm.tsx";
import Spinner from "../../components/Spinner.tsx";
import { useAuth } from "../../context/AuthContext.tsx";
import { useFlow } from "../../hooks/useFlow.ts";

/** The Kratos node carrying the address somebody registers with. */
const EMAIL_FIELD = "traits.email";

interface RegistrationFlowProps {
  /**
   * The invited address, pinned into the email field. The server refuses a
   * registration whose address does not match the invitation; the locked field
   * is how that rule is communicated rather than discovered.
   */
  lockedEmail?: string;
}

/**
 * RegistrationFlow is the Kratos registration form and nothing else.
 *
 * It is split out from the page around it for one reason, and it is a
 * correctness reason rather than a tidiness one: useFlow creates the
 * registration flow the moment it mounts, and in invite mode the proxy refuses
 * that request unless the bell_invite cookie is already set. Keeping the hook
 * behind a component that is only mounted once the invitation has been looked
 * up and the cookie written makes the ordering structural — there is no
 * arrangement of effects inside one component that would make it obvious, and
 * getting it wrong fails as a flow that cannot be created at all.
 */
export default function RegistrationFlow({ lockedEmail }: RegistrationFlowProps) {
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
    <>
      {error && (
        <div className="mb-4">
          <ErrorBanner message={error} />
        </div>
      )}
      {flow ? (
        <FlowForm
          flow={flow}
          onSubmit={handleSubmit}
          submitting={submitting}
          lockedValues={lockedEmail ? { [EMAIL_FIELD]: lockedEmail } : undefined}
        />
      ) : (
        <div className="flex justify-center py-8">
          <Spinner />
        </div>
      )}
    </>
  );
}
