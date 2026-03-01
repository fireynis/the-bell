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

const ACTION_COLORS: Record<string, string> = {
  warn: "bg-yellow-100 text-yellow-800",
  mute: "bg-orange-100 text-orange-800",
  suspend: "bg-red-100 text-red-800",
  ban: "bg-red-200 text-red-900",
};

interface ActionHistoryCardProps {
  entry: ActionHistoryEntry;
}

export default function ActionHistoryCard({ entry }: ActionHistoryCardProps) {
  const { action, penalties } = entry;
  const colorClass = ACTION_COLORS[action.action] ?? "bg-gray-100 text-gray-800";

  return (
    <article className="rounded-lg border border-gray-200 bg-white p-4 shadow-sm">
      <div className="mb-2 flex items-center justify-between">
        <span
          className={`rounded-full px-2 py-0.5 text-xs font-semibold ${colorClass}`}
        >
          {action.action.toUpperCase()}
        </span>
        <span className="text-xs text-gray-500">
          {formatDate(action.created_at)}
        </span>
      </div>

      <p className="mb-2 text-sm text-gray-700">
        <span className="font-medium">Reason:</span> {action.reason}
      </p>

      <div className="mb-2 flex flex-wrap gap-3 text-xs text-gray-500">
        <span>Severity: {action.severity}</span>
        <span>Moderator: {action.moderator_id.slice(0, 8)}</span>
        {action.expires_at && (
          <span>Expires: {formatDate(action.expires_at)}</span>
        )}
      </div>

      {/* Penalties */}
      {penalties.length > 0 && (
        <div className="mt-2 border-t border-gray-100 pt-2">
          <p className="mb-1 text-xs font-medium text-gray-600">
            Trust Penalties:
          </p>
          <div className="flex flex-col gap-1">
            {penalties.map((p) => (
              <div
                key={p.id}
                className="flex items-center justify-between text-xs text-gray-500"
              >
                <span>
                  {p.hop_depth === 0 ? "Direct" : `Hop ${p.hop_depth}`}:{" "}
                  -{p.penalty_amount.toFixed(1)} trust
                </span>
                {p.decays_at && (
                  <span className="text-gray-400">
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
