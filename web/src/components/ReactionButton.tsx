import { useCallback, useState } from "react";
import { api } from "../api/client";

const REACTION_EMOJI: Record<string, string> = {
  bell: "\uD83D\uDD14",
  heart: "\u2764\uFE0F",
  celebrate: "\uD83C\uDF89",
};

interface ReactionButtonProps {
  postId: string;
  type: string;
  count: number;
  active: boolean;
}

export default function ReactionButton({ postId, type, count, active: initialActive }: ReactionButtonProps) {
  const [localCount, setLocalCount] = useState(count);
  const [localActive, setLocalActive] = useState(initialActive);
  const [toggling, setToggling] = useState(false);

  const toggle = useCallback(async () => {
    if (toggling) return;
    setToggling(true);

    // Optimistic update
    const wasActive = localActive;
    setLocalActive(!wasActive);
    setLocalCount((c) => (wasActive ? c - 1 : c + 1));

    try {
      if (wasActive) {
        await api.delete(`/posts/${postId}/reactions/${type}`);
      } else {
        await api.post(`/posts/${postId}/reactions`, { type });
      }
    } catch {
      // Revert on failure
      setLocalActive(wasActive);
      setLocalCount((c) => (wasActive ? c + 1 : c - 1));
    } finally {
      setToggling(false);
    }
  }, [postId, type, localActive, toggling]);

  const emoji = REACTION_EMOJI[type] ?? type;

  return (
    <button
      type="button"
      onClick={toggle}
      disabled={toggling}
      className={`inline-flex items-center gap-1 rounded-full px-3 py-1 text-sm transition-colors ${
        localActive
          ? "bg-indigo-100 text-indigo-700"
          : "bg-gray-100 text-gray-600 hover:bg-gray-200"
      }`}
    >
      <span>{emoji}</span>
      {localCount > 0 && <span>{localCount}</span>}
    </button>
  );
}
