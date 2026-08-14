/**
 * The words the town uses for its own action, defined once.
 *
 * The same act was called three things — "Ring the Bell" in the sidebar, "Post"
 * on the submit button, "New Post" elsewhere — which reads as three features
 * rather than one. A control keeps its name through the whole flow: the button
 * that says "Ring the bell" is the one whose page is titled "Ring the bell".
 */

/** The action, wherever there is room to say it. */
export const RING_THE_BELL = "Ring the bell";

/** While the post is in flight, matching the verb above. */
export const RINGING = "Ringing…";

/**
 * The short form, for the bottom bar only.
 *
 * Five items at 360px is already tight; "Ring the bell" under an icon there
 * would wrap to three lines. This is the one place the short name is correct.
 */
export const POST_SHORT = "Post";

/** Appended when the member can see the action but cannot use it yet. */
export function ringTheBellLockedLabel(): string {
  return `${RING_THE_BELL} — you cannot post yet`;
}
