import { createContext, useContext, useEffect, useState, useCallback } from "react";
import type { ReactNode } from "react";
import type { KratosSession } from "../api/kratos-types.ts";
import type { User } from "../api/types.ts";
import { getSession, createLogoutFlow, performLogout } from "../api/kratos.ts";
import { onApiError, userApi } from "../api/client.ts";
import { isEmailUnverified } from "../lib/verification.ts";

interface AuthContextValue {
  session: KratosSession | null;
  user: User | null;
  /**
   * True when Kratos reports a live session but the member's profile could not
   * be read from our own API.
   *
   * `user === null` is not enough to tell that case from being signed out, and
   * the difference is the whole message: a signed-in member used to be told
   * "you must be signed in to post", which is both untrue and a loop they
   * cannot get out of. Pass it to the gating helpers as `profileUnavailable` so
   * the explanation matches what actually happened.
   */
  profileError: boolean;
  /**
   * True once the API has refused a request because this member's email address
   * has not been verified.
   *
   * It cannot be read off the profile: GET /v1/me is one of the two routes the
   * guard deliberately skips, so a member with an unverified address is signed
   * in, has a profile, and is refused everything else. The only evidence is a
   * refusal, which is why this is watched for rather than fetched — and why it
   * belongs to the session rather than to whichever page happened to make the
   * call that hit it.
   */
  emailUnverified: boolean;
  loading: boolean;
  refreshSession: () => Promise<void>;
  /**
   * Replaces the cached profile after the member edits their own.
   *
   * Cheaper than refreshSession and, unlike it, flicker-free: the server has
   * just answered with the updated user, so there is nothing left to ask. The
   * sidebar renders this copy, and before it existed a member changed their
   * display name and watched the old one sit in the corner until they signed in
   * again.
   *
   * An update naming somebody else is ignored, so no amount of editing a
   * neighbour's page can install them as the signed-in member.
   */
  updateUser: (updated: User) => void;
  logout: () => Promise<void>;
}

const AuthContext = createContext<AuthContextValue | null>(null);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [session, setSession] = useState<KratosSession | null>(null);
  const [user, setUser] = useState<User | null>(null);
  const [profileError, setProfileError] = useState(false);
  const [emailUnverified, setEmailUnverified] = useState(false);
  const [loading, setLoading] = useState(true);

  // Every refusal the API raises passes through here, and one of them is a fact
  // about the session rather than about the request that met it.
  useEffect(
    () =>
      onApiError((err) => {
        if (isEmailUnverified(err)) setEmailUnverified(true);
      }),
    [],
  );

  const refreshSession = useCallback(async () => {
    // Cleared before the reads that would set it again. A member who has just
    // verified their address gets an app that stops saying otherwise, without
    // this having to guess from a successful response which guard it passed.
    setEmailUnverified(false);
    try {
      const s = await getSession();
      setSession(s);
      if (s) {
        try {
          const u = await userApi.getMe();
          setUser(u);
          setProfileError(false);
        } catch {
          // A session with no profile behind it. Most often the backend user
          // row does not exist yet, moments after registration; it can equally
          // be the API being unreachable. Either way the caller is signed in,
          // so this is recorded rather than folded into "signed out".
          setUser(null);
          setProfileError(true);
        }
      } else {
        setUser(null);
        setProfileError(false);
      }
    } catch {
      // Kratos itself did not answer, so there is no session to speak of and no
      // profile failure to report either.
      setSession(null);
      setUser(null);
      setProfileError(false);
    } finally {
      setLoading(false);
    }
  }, []);

  const updateUser = useCallback((updated: User) => {
    // Matched on id rather than trusted: the only caller edits the member's own
    // profile, and this is what keeps a future caller on somebody else's page
    // from quietly swapping who the app thinks is signed in.
    setUser((current) => (current && current.id === updated.id ? updated : current));
  }, []);

  const logout = useCallback(async () => {
    try {
      const flow = await createLogoutFlow();
      await performLogout(flow.logout_token);
    } catch {
      // Logout may fail if session is already expired — that's fine
    }
    setSession(null);
    setUser(null);
    setProfileError(false);
    setEmailUnverified(false);
  }, []);

  useEffect(() => {
    refreshSession();
  }, [refreshSession]);

  return (
    <AuthContext
      value={{
        session,
        user,
        profileError,
        emailUnverified,
        loading,
        refreshSession,
        updateUser,
        logout,
      }}
    >
      {children}
    </AuthContext>
  );
}

// eslint-disable-next-line react-refresh/only-export-components
export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error("useAuth must be used within AuthProvider");
  return ctx;
}
