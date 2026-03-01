import type { ReactNode } from "react";

interface AuthLayoutProps {
  title: string;
  subtitle?: string;
  children: ReactNode;
  footer?: ReactNode;
}

export default function AuthLayout({ title, subtitle, children, footer }: AuthLayoutProps) {
  return (
    <div className="flex min-h-screen items-center justify-center bg-gray-50 px-4">
      <div className="w-full max-w-sm">
        <div className="mb-6 text-center">
          <h1 className="text-2xl font-bold text-gray-900">The Bell</h1>
        </div>
        <div className="rounded-lg bg-white p-6 shadow">
          <h2 className="mb-1 text-lg font-semibold text-gray-900">{title}</h2>
          {subtitle && <p className="mb-4 text-sm text-gray-500">{subtitle}</p>}
          {children}
        </div>
        {footer && <div className="mt-4 text-center text-sm text-gray-500">{footer}</div>}
      </div>
    </div>
  );
}
