import { Link, useLocation } from "react-router";
import { useAuth } from "../context/AuthContext";
import { useTheme } from "../context/ThemeContext";
import BellLogo from "./BellLogo";
import Avatar from "./Avatar";
import { NAV_ITEMS, SIDEBAR_NAV_ITEMS, isActive, navItemLocked, visibleNavItems } from "../lib/nav";
import { RING_THE_BELL, ringTheBellLockedLabel } from "../lib/copy";
import LockGlyph from "./LockGlyph";

export default function Sidebar() {
  const { user, logout } = useAuth();
  const { config } = useTheme();
  const location = useLocation();

  const visibleItems = visibleNavItems(SIDEBAR_NAV_ITEMS, user);
  // The sidebar's composer is a button of its own rather than a nav item, but
  // it leads to the same place and so carries the same mark.
  const composeLocked = navItemLocked(NAV_ITEMS.compose, user);

  return (
    <aside
      className="hidden lg:flex fixed left-0 top-0 h-screen flex-col justify-between border-r z-40"
      style={{
        width: "var(--sidebar-width)",
        backgroundColor: "var(--color-surface)",
        borderColor: "var(--color-border-light)",
      }}
    >
      {/* Top section */}
      <div className="flex flex-col gap-1 p-4">
        {/* Logo + town name */}
        <div className="flex items-center gap-3 px-3 py-2 mb-2">
          <BellLogo size={28} />
          <span
            className="text-lg font-bold truncate"
            style={{ fontFamily: "var(--font-display)", color: "var(--color-text)" }}
          >
            {config.town_name || "The Bell"}
          </span>
        </div>

        {/* Compose button */}
        <Link
          to="/compose"
          aria-label={composeLocked ? ringTheBellLockedLabel() : undefined}
          className="btn btn-primary mb-4 flex items-center justify-center gap-2 px-4 py-3 text-sm font-semibold"
          style={{
            // Kept in place and still followable — the composer is where the
            // reason lives — but visibly not the town's loudest button for
            // somebody who is not able to use it yet.
            opacity: composeLocked ? 0.65 : 1,
          }}
        >
          {composeLocked ? (
            <LockGlyph size={16} />
          ) : (
            <svg
              width="18"
              height="18"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="2"
              strokeLinecap="round"
              strokeLinejoin="round"
            >
              <path d="M18 8A6 6 0 0 0 6 8c0 7-3 9-3 9h18s-3-2-3-9" />
              <path d="M13.73 21a2 2 0 0 1-3.46 0" />
            </svg>
          )}
          {RING_THE_BELL}
        </Link>

        {/* Nav links */}
        <nav className="flex flex-col gap-1">
          {visibleItems.map((item) => {
            const active = isActive(item.path, location.pathname, item.exact);
            return (
              // aria-current is both the announcement and the styling hook: the
              // active item's colours come from it in CSS, so a link that reads
              // as current to a screen reader is the one that looks current.
              <Link
                key={item.path}
                to={item.path}
                aria-current={active ? "page" : undefined}
                className="nav-link flex items-center gap-3 rounded-[var(--radius-md)] px-3 py-2.5 text-sm font-medium"
              >
                <svg
                  width="20"
                  height="20"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  strokeWidth={active ? "2.5" : "2"}
                  strokeLinecap="round"
                  strokeLinejoin="round"
                >
                  <path d={item.icon} />
                </svg>
                {item.label}
              </Link>
            );
          })}
        </nav>
      </div>

      {/* Bottom section — user pill */}
      {user && (
        <div
          className="m-4 flex items-center gap-3 rounded-lg border p-3"
          style={{
            borderColor: "var(--color-border-light)",
            backgroundColor: "var(--color-surface)",
          }}
        >
          <Avatar url={user.avatar_url || ""} name={user.display_name || ""} size="sm" />
          <div className="flex-1 min-w-0">
            <div
              className="text-sm font-semibold truncate"
              style={{ color: "var(--color-text)" }}
            >
              {user.display_name || "User"}
            </div>
            <div
              className="text-xs capitalize truncate"
              style={{ color: "var(--color-text-secondary)" }}
            >
              {user.role}
            </div>
          </div>
          <button
            onClick={logout}
            className="tint-danger-on-hover flex-shrink-0 rounded-md p-1.5"
            style={{ color: "var(--color-text-tertiary)" }}
            aria-label="Sign out"
            title="Sign out"
          >
            <svg
              width="18"
              height="18"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="2"
              strokeLinecap="round"
              strokeLinejoin="round"
            >
              <path d="M17 16l4-4m0 0l-4-4m4 4H7m6 4v1a3 3 0 01-3 3H6a3 3 0 01-3-3V7a3 3 0 013-3h4a3 3 0 013 3v1" />
            </svg>
          </button>
        </div>
      )}
    </aside>
  );
}
