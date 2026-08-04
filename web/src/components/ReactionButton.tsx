import { useCallback, useState } from "react";
import { api } from "../api/client";
import { reactionEmoji, revertReaction, toggleReaction, type ReactionState } from "../lib/reactions";

interface ReactionButtonProps {
  postId: string;
  type: string;
  count: number;
  active: boolean;
}

export default function ReactionButton({ postId, type, count, active }: ReactionButtonProps) {
  const [state, setState] = useState<ReactionState>({ count, active });
  const [toggling, setToggling] = useState(false);

  // The optimistic state is seeded from the props, so it has to be re-seeded
  // when they change: a feed refresh carrying a count updated by other readers
  // would otherwise be shown the stale number this button started with.
  const [seeded, setSeeded] = useState<ReactionState>({ count, active });
  if (seeded.count !== count || seeded.active !== active) {
    setSeeded({ count, active });
    setState({ count, active });
  }

  const toggle = useCallback(async () => {
    if (toggling) return;
    setToggling(true);

    const before = state;
    setState(toggleReaction(before));

    try {
      if (before.active) {
        await api.delete(`/posts/${postId}/reactions/${type}`);
      } else {
        await api.post(`/posts/${postId}/reactions`, { type });
      }
    } catch {
      setState((current) => revertReaction(current, before));
    } finally {
      setToggling(false);
    }
  }, [postId, type, state, toggling]);

  const emoji = reactionEmoji(type);

  return (
    <button
      type="button"
      onClick={toggle}
      disabled={toggling}
      className="inline-flex items-center gap-1 rounded-full px-3 py-1 text-sm transition-colors"
      style={{
        backgroundColor: state.active ? "var(--color-primary-light)" : "var(--color-surface-tertiary)",
        color: state.active ? "var(--color-primary)" : "var(--color-text-secondary)",
      }}
      onMouseEnter={(e) => {
        if (!state.active) e.currentTarget.style.backgroundColor = "var(--color-surface-hover)";
      }}
      onMouseLeave={(e) => {
        if (!state.active) e.currentTarget.style.backgroundColor = "var(--color-surface-tertiary)";
      }}
    >
      <span>{emoji}</span>
      {state.count > 0 && <span>{state.count}</span>}
    </button>
  );
}
