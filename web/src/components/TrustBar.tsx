import { clampTrustScore, trustTier } from "../lib/trust";

const TIER_COLORS = {
  high: "var(--color-success)",
  medium: "var(--color-warning)",
  low: "var(--color-danger)",
} as const;

export default function TrustBar({ score }: { score: number }) {
  const clamped = clampTrustScore(score);

  return (
    <div className="flex items-center gap-2">
      <span className="text-xs font-medium" style={{ color: "var(--color-text-secondary)" }}>Trust</span>
      <div className="h-1.5 w-20 rounded-full" style={{ backgroundColor: "var(--color-border-light)" }}>
        <div
          className="h-1.5 rounded-full transition-all duration-500"
          style={{ width: `${clamped}%`, backgroundColor: TIER_COLORS[trustTier(clamped)] }}
        />
      </div>
      <span className="text-xs font-medium" style={{ color: "var(--color-text-secondary)" }}>
        {Math.round(clamped)}
      </span>
    </div>
  );
}
