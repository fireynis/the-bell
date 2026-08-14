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

/** What each reaction is called out loud, where the emoji is the visible label. */
const REACTION_NAME: Record<string, string> = {
  bell: "Bell",
  heart: "Heart",
  celebrate: "Celebrate",
};

/** Said for a reaction type this build has no name for, as reactionEmoji does. */
const DEFAULT_NAME = "Reaction";

/**
 * reactionLabel names a reaction button for a screen reader.
 *
 * The button shows an emoji and a bare number, which is read out as "bell
 * three" — neither what the control is nor what the number counts. The name
 * says both; whether the reader has already reacted is carried separately by
 * aria-pressed, so it is deliberately not repeated here.
 */
export function reactionLabel(reactionType: string, count: number): string {
  const name = REACTION_NAME[reactionType] ?? DEFAULT_NAME;
  if (count === 0) return `${name}, no reactions`;
  if (count === 1) return `${name}, 1 reaction`;
  return `${name}, ${count} reactions`;
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
