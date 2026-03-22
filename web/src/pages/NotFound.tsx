import { Link } from "react-router";
import BellLogo from "../components/BellLogo";

export default function NotFound() {
  return (
    <div
      className="flex min-h-screen flex-col items-center justify-center gap-4 px-4"
      style={{ backgroundColor: "var(--color-surface-secondary)" }}
    >
      <BellLogo size={48} className="opacity-30" />
      <h1
        className="text-4xl font-bold"
        style={{ fontFamily: "var(--font-display)", color: "var(--color-text)" }}
      >
        404
      </h1>
      <p style={{ color: "var(--color-text-secondary)" }}>
        This bell doesn't ring here.
      </p>
      <Link
        to="/"
        className="mt-2 rounded-full px-5 py-2 text-sm font-semibold"
        style={{
          backgroundColor: "var(--color-primary)",
          color: "var(--color-text-inverse)",
        }}
      >
        Back to town
      </Link>
    </div>
  );
}
