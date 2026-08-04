import { Link, useLocation } from "react-router";
import { useAuth } from "../context/AuthContext";
import { BOTTOM_NAV_ITEMS, isActive, visibleNavItems } from "../lib/nav";

export default function BottomNav() {
  const { user } = useAuth();
  const location = useLocation();

  const items = visibleNavItems(BOTTOM_NAV_ITEMS, user);

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
