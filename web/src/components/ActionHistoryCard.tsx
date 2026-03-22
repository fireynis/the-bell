import type { ActionHistoryEntry } from "../api/types.ts";

function formatDate(dateStr: string): string {
  return new Date(dateStr).toLocaleDateString(undefined, {
    year: "numeric",
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

const ACTION_BADGE_STYLES: Record<string, React.CSSProperties> = {
  warn: {
    backgroundColor: "var(--color-warning-light)",
    color: "var(--color-warning)",
  },
  mute: {
    backgroundColor: "var(--color-accent-light)",
    color: "var(--color-accent)",
  },
  suspend: {
    backgroundColor: "var(--color-danger-light)",
    color: "var(--color-danger)",
  },
  ban: {
    backgroundColor: "var(--color-danger-light)",
    color: "var(--color-danger)",
  },
};

interface ActionHistoryCardProps {
  entry: ActionHistoryEntry;
}

export default function ActionHistoryCard({ entry }: ActionHistoryCardProps) {
  const { action, penalties } = entry;
  const badgeStyle: React.CSSProperties = ACTION_BADGE_STYLES[action.action] ?? {
    backgroundColor: "var(--color-surface-secondary)",
    color: "var(--color-text-secondary)",
  };

  return (
    <article
      className="rounded-lg border p-4"
      style={{
        backgroundColor: "var(--color-surface)",
        boxShadow: "var(--shadow-sm)",
        borderRadius: "var(--radius-lg)",
        borderWidth: "1px",
        borderColor: "var(--color-border-light)",
      }}
    >
      <div className="mb-2 flex items-center justify-between">
        <span
          className="rounded-full px-2 py-0.5 text-xs font-semibold"
          style={badgeStyle}
        >
          {action.action.toUpperCase()}
        </span>
        <span className="text-xs" style={{ color: "var(--color-text-tertiary)" }}>
          {formatDate(action.created_at)}
        </span>
      </div>

      <p className="mb-2 text-sm" style={{ color: "var(--color-text-secondary)" }}>
        <span className="font-medium">Reason:</span> {action.reason}
      </p>

      <div
        className="mb-2 flex flex-wrap gap-3 text-xs"
        style={{ color: "var(--color-text-tertiary)" }}
      >
        <span>Severity: {action.severity}</span>
        <span>Moderator: {action.moderator_id.slice(0, 8)}</span>
        {action.expires_at && (
          <span>Expires: {formatDate(action.expires_at)}</span>
        )}
      </div>

      {/* Penalties */}
      {penalties.length > 0 && (
        <div
          className="mt-2 border-t pt-2"
          style={{ borderColor: "var(--color-border-light)" }}
        >
          <p
            className="mb-1 text-xs font-medium"
            style={{ color: "var(--color-text-secondary)" }}
          >
            Trust Penalties:
          </p>
          <div className="flex flex-col gap-1">
            {penalties.map((p) => (
              <div
                key={p.id}
                className="flex items-center justify-between text-xs"
                style={{ color: "var(--color-text-tertiary)" }}
              >
                <span>
                  {p.hop_depth === 0 ? "Direct" : `Hop ${p.hop_depth}`}:{" "}
                  -{p.penalty_amount.toFixed(1)} trust
                </span>
                {p.decays_at && (
                  <span style={{ color: "var(--color-text-tertiary)" }}>
                    Decays {formatDate(p.decays_at)}
                  </span>
                )}
              </div>
            ))}
          </div>
        </div>
      )}
    </article>
  );
}
