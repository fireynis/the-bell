/** Mirrors the ReactionType constants in internal/domain/reaction.go. */
export const REACTION_TYPES = ["bell", "heart", "celebrate"] as const;

export type ReactionType = (typeof REACTION_TYPES)[number];

const REACTION_EMOJI: Record<string, string> = {
  bell: "🔔",
  heart: "❤️",
  celebrate: "🎉",
};

/** Shown for a reaction type this build does not know the emoji for. */
const DEFAULT_EMOJI = "👍";

/**
 * reactionEmoji renders a reaction type as the glyph readers recognise.
 *
 * The server can start emitting a reaction type older clients have never seen,
 * so an unknown type falls back to a generic emoji rather than leaking the raw
 * type name into the UI.
 */
export function reactionEmoji(reactionType: string): string {
  return REACTION_EMOJI[reactionType] ?? DEFAULT_EMOJI;
}

/** A single reaction button's view of one reaction type on one post. */
export interface ReactionState {
  count: number;
  active: boolean;
}

/**
 * toggleReaction computes the optimistic state shown the instant a reader taps,
 * before the server has confirmed anything.
 *
 * The count is floored at zero: a stale count of 0 with the reaction already
 * marked active would otherwise render "-1", which is not a thing a post can
 * have.
 */
export function toggleReaction(state: ReactionState): ReactionState {
  return state.active
    ? { count: Math.max(0, state.count - 1), active: false }
    : { count: state.count + 1, active: true };
}

/**
 * revertReaction undoes an optimistic toggle after the request failed, putting
 * back the snapshot taken before it.
 *
 * The snapshot is restored wholesale rather than by applying the inverse delta,
 * because a delta applied to a state that has already moved on drifts away from
 * the truth. For the same reason a state already back on the snapshot's side is
 * left alone, so a double revert cannot double-count.
 */
export function revertReaction(state: ReactionState, before: ReactionState): ReactionState {
  if (state.active === before.active) return state;
  return { count: Math.max(0, before.count), active: before.active };
}
