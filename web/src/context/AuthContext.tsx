import { createContext, useContext, useEffect, useState, useCallback } from "react";
import type { ReactNode } from "react";
import type { KratosSession } from "../api/kratos-types.ts";
import type { User } from "../api/types.ts";
import { getSession, createLogoutFlow, performLogout } from "../api/kratos.ts";
import { userApi } from "../api/client.ts";

interface AuthContextValue {
  session: KratosSession | null;
  user: User | null;
  loading: boolean;
  refreshSession: () => Promise<void>;
  logout: () => Promise<void>;
}

const AuthContext = createContext<AuthContextValue | null>(null);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [session, setSession] = useState<KratosSession | null>(null);
  const [user, setUser] = useState<User | null>(null);
  const [loading, setLoading] = useState(true);

  const refreshSession = useCallback(async () => {
    try {
      const s = await getSession();
      setSession(s);
      if (s) {
        try {
          const u = await userApi.getMe();
          setUser(u);
        } catch {
          // User may not exist in backend yet (pending sync)
          setUser(null);
        }
      } else {
        setUser(null);
      }
    } catch {
      setSession(null);
      setUser(null);
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
  }, []);

  useEffect(() => {
    refreshSession();
  }, [refreshSession]);

  return (
    <AuthContext value={{ session, user, loading, refreshSession, logout }}>
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
