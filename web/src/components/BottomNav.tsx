import { Link, useLocation } from "react-router";
import { useAuth } from "../context/AuthContext";

interface BottomNavItem {
  label: string;
  path: string;
  icon: string;
  exact?: boolean;
  roles?: string[];
}

const baseItems: BottomNavItem[] = [
  {
    label: "Feed",
    path: "/",
    icon: "M3 12l2-2m0 0l7-7 7 7M5 10v10a1 1 0 001 1h3m10-11l2 2m-2-2v10a1 1 0 01-1 1h-3m-4 0a1 1 0 01-1-1v-4a1 1 0 011-1h2a1 1 0 011 1v4a1 1 0 01-1 1",
    exact: true,
  },
  {
    label: "Post",
    path: "/compose",
    icon: "M12 4v16m8-8H4",
  },
];

const moderationItem: BottomNavItem = {
  label: "Moderation",
  path: "/moderation",
  icon: "M9 12l2 2 4-4m5.618-4.016A11.955 11.955 0 0112 2.944a11.955 11.955 0 01-8.618 3.04A12.02 12.02 0 003 9c0 5.591 3.824 10.29 9 11.622 5.176-1.332 9-6.03 9-11.622 0-1.042-.133-2.052-.382-3.016z",
  roles: ["moderator", "council"],
};

const trailingItems: BottomNavItem[] = [
  {
    label: "Profile",
    path: "/profile",
    icon: "M16 7a4 4 0 11-8 0 4 4 0 018 0zM12 14a7 7 0 00-7 7h14a7 7 0 00-7-7z",
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

export default function BottomNav() {
  const { user } = useAuth();
  const location = useLocation();

  const showModeration =
    user?.role && moderationItem.roles?.includes(user.role);

  const items: BottomNavItem[] = [
    ...baseItems,
    ...(showModeration ? [moderationItem] : []),
    ...trailingItems,
  ];

  return (
    <nav
      className="fixed bottom-0 left-0 right-0 z-40 flex items-center justify-around border-t lg:hidden"
      style={{
        backgroundColor: "var(--color-surface)",
        borderColor: "var(--color-border-light)",
        paddingBottom: "env(safe-area-inset-bottom)",
      }}
    >
      {items.map((item) => {
        const active = isActive(item.path, location.pathname, item.exact);
        return (
          <Link
            key={item.path}
            to={item.path}
            className="flex flex-col items-center gap-1 px-3 py-2 text-[10px] font-medium"
            style={{
              color: active ? "var(--color-primary)" : "var(--color-text-tertiary)",
            }}
          >
            <svg
              width="22"
              height="22"
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
  );
}
