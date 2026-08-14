import { Outlet } from "react-router";
import Sidebar from "./Sidebar";
import BottomNav from "./BottomNav";
import ErrorBanner from "./ErrorBanner";
import { useTheme } from "../context/ThemeContext";
import { useAuth } from "../context/AuthContext";
import BellLogo from "./BellLogo";

export default function AppLayout() {
  const { config } = useTheme();
  const { profileError, refreshSession } = useAuth();

  return (
    <div className="min-h-screen" style={{ backgroundColor: "var(--color-surface-secondary)" }}>
      <Sidebar />
      <BottomNav />

      {/* Mobile header */}
      <header
        className="sticky top-0 z-30 flex items-center justify-between border-b px-4 py-3 lg:hidden"
        style={{
          backgroundColor: "var(--color-surface)",
          borderColor: "var(--color-border-light)",
        }}
      >
        <div className="flex items-center gap-2">
          <BellLogo size={22} />
          <span className="text-base font-bold" style={{ fontFamily: "var(--font-display)" }}>
            {config.town_name || "The Bell"}
          </span>
        </div>
      </header>

      {/* Main content */}
      <main className="pb-20 lg:pb-0 lg:pl-[var(--sidebar-width)]">
        <div className="mx-auto px-4" style={{ maxWidth: "var(--content-max-width)" }}>
          {/*
            Said once, at the top of every page, because the symptom shows up
            everywhere: the sidebar loses its user pill, the composer refuses,
            and each of those on its own reads as being signed out. Retry is
            refreshSession, which is exactly the request that failed.
          */}
          {profileError && (
            <div className="pt-4">
              <ErrorBanner
                message="We could not load your account just now. Some things will look as though you are signed out until it loads."
                onRetry={() => void refreshSession()}
              />
            </div>
          )}
          <Outlet />
        </div>
      </main>
    </div>
  );
}
