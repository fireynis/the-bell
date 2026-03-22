import { Outlet } from "react-router";
import Sidebar from "./Sidebar";
import BottomNav from "./BottomNav";
import { useTheme } from "../context/ThemeContext";
import BellLogo from "./BellLogo";

export default function AppLayout() {
  const { config } = useTheme();

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
          <Outlet />
        </div>
      </main>
    </div>
  );
}
