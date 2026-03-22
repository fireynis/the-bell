import { useState } from "react";
import { Link, useLocation } from "react-router";
import { useAuth } from "../context/AuthContext";
import { useTheme } from "../context/ThemeContext";
import BellLogo from "./BellLogo";
import Avatar from "./Avatar";

interface NavItem {
  label: string;
  path: string;
  icon: string;
  exact?: boolean;
  roles?: string[];
}

const navItems: NavItem[] = [
  {
    label: "Feed",
    path: "/",
    icon: "M3 12l2-2m0 0l7-7 7 7M5 10v10a1 1 0 001 1h3m10-11l2 2m-2-2v10a1 1 0 01-1 1h-3m-4 0a1 1 0 01-1-1v-4a1 1 0 011-1h2a1 1 0 011 1v4a1 1 0 01-1 1",
    exact: true,
  },
  {
    label: "Profile",
    path: "/profile",
    icon: "M16 7a4 4 0 11-8 0 4 4 0 018 0zM12 14a7 7 0 00-7 7h14a7 7 0 00-7-7z",
  },
  {
    label: "Moderation",
    path: "/moderation",
    icon: "M9 12l2 2 4-4m5.618-4.016A11.955 11.955 0 0112 2.944a11.955 11.955 0 01-8.618 3.04A12.02 12.02 0 003 9c0 5.591 3.824 10.29 9 11.622 5.176-1.332 9-6.03 9-11.622 0-1.042-.133-2.052-.382-3.016z",
    roles: ["moderator", "council"],
  },
  {
    label: "Town Hall",
    path: "/admin",
    icon: "M19 21V5a2 2 0 00-2-2H7a2 2 0 00-2 2v16m14 0h2m-2 0h-5m-9 0H3m2 0h5M9 7h1m-1 4h1m4-4h1m-1 4h1m-5 10v-5a1 1 0 011-1h2a1 1 0 011 1v5m-4 0h4",
    roles: ["council"],
  },
  {
    label: "Settings",
    path: "/auth/settings",
    icon: "M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.066 2.573c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.573 1.066c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.066-2.573c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z M15 12a3 3 0 11-6 0 3 3 0 016 0z",
  },
];

function isActive(path: string, currentPath: string, exact?: boolean): boolean {
  if (exact) return currentPath === path;
  return currentPath.startsWith(path);
}

export default function Sidebar() {
  const { user, logout } = useAuth();
  const { config } = useTheme();
  const location = useLocation();
  const [hoveredItem, setHoveredItem] = useState<string | null>(null);
  const [composeHovered, setComposeHovered] = useState(false);
  const [signOutHovered, setSignOutHovered] = useState(false);

  const visibleItems = navItems.filter((item) => {
    if (!item.roles) return true;
    return user?.role && item.roles.includes(user.role);
  });

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
          className="flex items-center justify-center gap-2 rounded-full px-4 py-3 mb-4 text-sm font-semibold transition-colors"
          style={{
            backgroundColor: composeHovered ? "var(--color-primary-hover)" : "var(--color-primary)",
            color: "var(--color-text-inverse)",
            borderRadius: "var(--radius-md)",
          }}
          onMouseEnter={() => setComposeHovered(true)}
          onMouseLeave={() => setComposeHovered(false)}
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
            <path d="M18 8A6 6 0 0 0 6 8c0 7-3 9-3 9h18s-3-2-3-9" />
            <path d="M13.73 21a2 2 0 0 1-3.46 0" />
          </svg>
          Ring the Bell
        </Link>

        {/* Nav links */}
        <nav className="flex flex-col gap-1">
          {visibleItems.map((item) => {
            const active = isActive(item.path, location.pathname, item.exact);
            const hovered = hoveredItem === item.path;
            return (
              <Link
                key={item.path}
                to={item.path}
                className="flex items-center gap-3 rounded-lg px-3 py-2.5 text-sm font-medium transition-colors"
                style={{
                  backgroundColor: active
                    ? "var(--color-primary-light)"
                    : hovered
                      ? "var(--color-surface-hover)"
                      : "transparent",
                  color: active ? "var(--color-primary)" : "var(--color-text)",
                  borderRadius: "var(--radius-md)",
                }}
                onMouseEnter={() => setHoveredItem(item.path)}
                onMouseLeave={() => setHoveredItem(null)}
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
            className="flex-shrink-0 rounded-md p-1.5 transition-colors"
            style={{
              color: signOutHovered ? "var(--color-danger)" : "var(--color-text-tertiary)",
            }}
            onMouseEnter={() => setSignOutHovered(true)}
            onMouseLeave={() => setSignOutHovered(false)}
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
