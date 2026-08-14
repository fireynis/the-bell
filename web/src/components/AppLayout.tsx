import { Link, Outlet } from "react-router";
import Sidebar from "./Sidebar";
import BottomNav from "./BottomNav";
import ErrorBanner from "./ErrorBanner";
import { useTheme } from "../context/ThemeContext";
import { useAuth } from "../context/AuthContext";
import { NAV_ITEMS } from "../lib/nav";
import { UNVERIFIED_EMAIL_NOTICE, VERIFICATION_PATH } from "../lib/verification";
import BellLogo from "./BellLogo";

interface AppLayoutProps {
  /**
   * Widens the content column past the reading measure.
   *
   * The default 600px is set for a column of posts, which is the whole app bar
   * one page: Town Hall is a dashboard of counts and tables, and its four-across
   * stat row was being crushed into two cramped columns at every width. Passed
   * per route in routes.tsx rather than sniffed from the path, so the layout
   * never has to know which pages exist.
   */
  wide?: boolean;
}

export default function AppLayout({ wide = false }: AppLayoutProps) {
  const { config } = useTheme();
  const { profileError, emailUnverified, refreshSession } = useAuth();

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
        {/*
          The one horizontal gutter in the app. Pages used to add their own
          `mx-auto max-w-2xl p-4` inside this, which doubled the padding and
          narrowed the column a second time.
        */}
        <div
          className="mx-auto px-4"
          style={{
            maxWidth: wide ? "var(--content-max-width-wide)" : "var(--content-max-width)",
          }}
        >
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

          {/*
            Also said once, at the top of every page, and for the same reason:
            an unverified member is signed in and refused everywhere, so each
            page on its own could only report the failure it happened to meet.

            Warm rather than alarming — nothing has gone wrong, there is simply
            a message waiting in their inbox — so it borrows the accent colours
            the mute banner uses rather than the danger red of a real error.
          */}
          {emailUnverified && (
            <div
              className="mt-4 rounded-[var(--radius-md)] p-3 text-sm"
              style={{
                backgroundColor: "var(--color-accent-light)",
                color: "var(--color-accent)",
              }}
              role="status"
            >
              {UNVERIFIED_EMAIL_NOTICE}{" "}
              <Link to={VERIFICATION_PATH} className="font-semibold underline">
                Verify your email
              </Link>
            </div>
          )}
          <Outlet />
        </div>
      </main>
    </div>
  );
}
