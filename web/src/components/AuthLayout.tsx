import type { ReactNode } from "react";
import BellLogo from "./BellLogo.tsx";
import { useTheme } from "../context/ThemeContext.tsx";

interface AuthLayoutProps {
  title: string;
  subtitle?: string;
  children: ReactNode;
  footer?: ReactNode;
}

export default function AuthLayout({ title, subtitle, children, footer }: AuthLayoutProps) {
  const { config } = useTheme();

  return (
    <div
      className="flex min-h-screen items-center justify-center px-4"
      style={{ backgroundColor: "var(--color-surface-secondary)" }}
    >
      <div className="animate-fade-in-up w-full max-w-sm">
        <div className="mb-6 flex flex-col items-center gap-2 text-center">
          <BellLogo size={40} animate />
          <span
            className="text-2xl font-bold"
            style={{ fontFamily: "var(--font-display)", color: "var(--color-text)" }}
          >
            {config.town_name}
          </span>
        </div>
        <div
          className="rounded-[var(--radius-lg)] p-6"
          style={{
            backgroundColor: "var(--color-surface)",
            boxShadow: "var(--shadow-lg)",
          }}
        >
          <h2
            className="mb-1 text-lg font-semibold"
            style={{ color: "var(--color-text)" }}
          >
            {title}
          </h2>
          {subtitle && (
            <p className="mb-4 text-sm" style={{ color: "var(--color-text-secondary)" }}>
              {subtitle}
            </p>
          )}
          {children}
        </div>
        {footer && (
          <div
            className="mt-4 text-center text-sm"
            style={{ color: "var(--color-text-secondary)" }}
          >
            {footer}
          </div>
        )}
      </div>
    </div>
  );
}
