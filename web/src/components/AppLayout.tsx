import { Link, Outlet } from "react-router";
import Sidebar from "./Sidebar";
import BottomNav from "./BottomNav";
import ErrorBanner from "./ErrorBanner";
import { useTheme } from "../context/ThemeContext";
import { useAuth } from "../context/AuthContext";
import { NAV_ITEMS } from "../lib/nav";
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

        {/*
          The mobile route to a member's own account. The sidebar that carries
          Settings everywhere else is `hidden lg:flex`, so this is the only one
          a phone has — the bottom bar gave the sixth slot to Neighbours, and
          without this there would be no way to reach it at all below lg.
        */}
        <Link
          to={NAV_ITEMS.settings.path}
          aria-label={NAV_ITEMS.settings.label}
          className="rounded-md p-1.5"
          style={{ color: "var(--color-text-tertiary)" }}
        >
          <svg
            width="20"
            height="20"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="2"
            strokeLinecap="round"
            strokeLinejoin="round"
            aria-hidden="true"
          >
            <path d={NAV_ITEMS.settings.icon} />
          </svg>
        </Link>
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
