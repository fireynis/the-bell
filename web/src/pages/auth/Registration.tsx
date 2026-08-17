import { useEffect, useState } from "react";
import { Link, useSearchParams } from "react-router";
import { inviteApi } from "../../api/client.ts";
import type { ApiError, InviteLookup } from "../../api/types.ts";
import AuthLayout from "../../components/AuthLayout.tsx";
import Spinner from "../../components/Spinner.tsx";
import { useTheme } from "../../context/ThemeContext.tsx";
import {
  INVITE_EXPIRED_NOTICE,
  INVITE_ONLY_NOTICE,
  invitedEmailNote,
  invitedGreeting,
  registrationMode,
  setInviteCookie,
} from "../../lib/invite.ts";
import RegistrationFlow from "./RegistrationFlow.tsx";

/**
 * What the page knows about the invitation in the URL.
 *
 * "gone" and "unverified" are kept apart because they call for opposite
 * treatment. A 404 is the server saying this link is no good — used, withdrawn,
 * lapsed or invented, deliberately indistinguishable — and the visitor should
 * be told so. Any other failure is our own end not answering, and refusing to
 * show a form on that basis would lock out an invited neighbour over a blip; the
 * cookie is set and the server, which is the authority, decides.
 */
type InviteState =
  | { status: "none" }
  | { status: "checking" }
  | { status: "valid"; invite: InviteLookup }
  | { status: "gone" }
  | { status: "unverified" };

/**
 * Registration decides who may see a registration form at all.
 *
 * In a town that admits people by invitation there are three arrivals here and
 * they want three different pages: somebody following a live invitation, who
 * gets a greeting and a form with their address already in it; somebody
 * following one that has stopped working, who needs to know that is what
 * happened rather than that they did something wrong; and somebody who arrived
 * with no invitation at all, who gets an explanation and no form, because a
 * form they cannot submit is a worse answer than no form.
 *
 * In open mode none of that applies and the page is what it always was.
 */
export default function Registration() {
  const [searchParams] = useSearchParams();
  const token = (searchParams.get("invite") ?? "").trim();
  const { config, loading: configLoading } = useTheme();
  const mode = registrationMode(config);

  /**
   * The answer to the lookup, tagged with the token it answers about.
   *
   * Tagged rather than reset by the effect, because "which token is this the
   * answer to" is the whole question: without it, a page whose URL changed
   * would show the previous invitation's greeting until the new lookup landed.
   * Deriving the state from it also keeps the effect free of a synchronous
   * setState, which would re-render the page twice on every load.
   */
  const [resolved, setResolved] = useState<{ token: string; state: InviteState } | null>(null);

  const state: InviteState = !token
    ? { status: "none" }
    : resolved?.token === token
      ? resolved.state
      : { status: "checking" };

  useEffect(() => {
    if (!token) return;

    let cancelled = false;

    inviteApi
      .lookup(token)
      .then((invite) => {
        if (cancelled) return;
        // Written before the state change that mounts RegistrationFlow, which
        // is what creates the Kratos flow: the proxy reads this cookie on that
        // request, so a cookie set afterwards is a cookie set too late.
        setInviteCookie(token);
        setResolved({ token, state: { status: "valid", invite } });
      })
      .catch((err: ApiError) => {
        if (cancelled) return;
        if (err?.status === 404) {
          setResolved({ token, state: { status: "gone" } });
          return;
        }
        setInviteCookie(token);
        setResolved({ token, state: { status: "unverified" } });
      });

    return () => {
      cancelled = true;
    };
  }, [token]);

  const townName = config.town_name ?? "the town";
  const signIn = (
    <Link to="/auth/login" style={{ color: "var(--color-primary)" }}>
      {INVITE_ONLY_NOTICE.signIn}
    </Link>
  );

  // The config decides whether there is a form here at all, so a page rendered
  // before it lands would show one and take it away again. An invitation that
  // has already checked out needs no such wait: it is a form either way.
  if (state.status === "checking" || (configLoading && state.status !== "valid")) {
    return (
      <AuthLayout title="Create an account" footer={signIn}>
        <div className="flex justify-center py-8">
          <Spinner />
        </div>
      </AuthLayout>
    );
  }

  if (state.status === "valid") {
    return (
      <AuthLayout title="Create an account" footer={signIn}>
        <div
          className="mb-4 rounded-[var(--radius-md)] p-3"
          style={{
            backgroundColor: "var(--color-primary-subtle)",
            border: "1px solid var(--color-primary-light)",
          }}
          data-testid="invited-greeting"
        >
          <p className="text-sm font-medium" style={{ color: "var(--color-text)" }}>
            {invitedGreeting(state.invite.inviter_display_name, state.invite.town_name || townName)}
          </p>
          <p className="mt-1 text-xs" style={{ color: "var(--color-text-secondary)" }}>
            {invitedEmailNote(state.invite.email)}
          </p>
        </div>
        <RegistrationFlow lockedEmail={state.invite.email} />
      </AuthLayout>
    );
  }

  // No usable invitation, and the town only admits people who have one. There
  // is nothing to submit, so there is no form — just what to do about it.
  if (mode === "invite" && state.status !== "unverified") {
    return (
      <AuthLayout title={INVITE_ONLY_NOTICE.title} footer={signIn}>
        {state.status === "gone" && (
          <p
            className="mb-4 rounded-[var(--radius-md)] p-3 text-sm"
            style={{
              backgroundColor: "var(--color-danger-light)",
              color: "var(--color-danger)",
            }}
            role="status"
          >
            {INVITE_EXPIRED_NOTICE}
          </p>
        )}
        <p className="text-sm leading-relaxed" style={{ color: "var(--color-text-secondary)" }}>
          {INVITE_ONLY_NOTICE.body(townName)}
        </p>
        <p
          className="mt-3 text-sm leading-relaxed"
          style={{ color: "var(--color-text-secondary)" }}
        >
          {INVITE_ONLY_NOTICE.ask(townName)}
        </p>
      </AuthLayout>
    );
  }

  return (
    <AuthLayout title="Create an account" subtitle={`Join ${townName}`} footer={signIn}>
      {/*
        An open town, reached with a link that has stopped working. The form is
        theirs to use either way — the invitation was never what let them in —
        but saying nothing would leave them wondering what the link was for.
      */}
      {state.status === "gone" && (
        <p
          className="mb-4 rounded-[var(--radius-md)] p-3 text-sm"
          style={{ backgroundColor: "var(--color-danger-light)", color: "var(--color-danger)" }}
          role="status"
        >
          {INVITE_EXPIRED_NOTICE}
        </p>
      )}
      <RegistrationFlow />
    </AuthLayout>
  );
}
