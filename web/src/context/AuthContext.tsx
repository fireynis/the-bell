import { createContext, useContext, useEffect, useState, useCallback } from "react";
import type { ReactNode } from "react";
import type { KratosSession } from "../api/kratos-types.ts";
import type { User } from "../api/types.ts";
import { getSession, createLogoutFlow, performLogout } from "../api/kratos.ts";
import { userApi } from "../api/client.ts";

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
  loading: boolean;
  refreshSession: () => Promise<void>;
  logout: () => Promise<void>;
}

const AuthContext = createContext<AuthContextValue | null>(null);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [session, setSession] = useState<KratosSession | null>(null);
  const [user, setUser] = useState<User | null>(null);
  const [profileError, setProfileError] = useState(false);
  const [loading, setLoading] = useState(true);

  const refreshSession = useCallback(async () => {
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
  }, []);

  useEffect(() => {
    refreshSession();
  }, [refreshSession]);

  return (
    <AuthContext value={{ session, user, profileError, loading, refreshSession, logout }}>
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
