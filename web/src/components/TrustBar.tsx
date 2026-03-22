export default function TrustBar({ score }: { score: number }) {
  const clamped = Math.max(0, Math.min(100, score));
  const barColor =
    clamped >= 60 ? "var(--color-success)" :
    clamped >= 30 ? "var(--color-warning)" :
    "var(--color-danger)";

  return (
    <div className="flex items-center gap-2">
      <span className="text-xs font-medium" style={{ color: "var(--color-text-secondary)" }}>Trust</span>
      <div className="h-1.5 w-20 rounded-full" style={{ backgroundColor: "var(--color-border-light)" }}>
        <div
          className="h-1.5 rounded-full transition-all duration-500"
          style={{ width: `${clamped}%`, backgroundColor: barColor }}
        />
      </div>
      <span className="text-xs font-medium" style={{ color: "var(--color-text-secondary)" }}>
        {Math.round(clamped)}
      </span>
    </div>
  );
}
