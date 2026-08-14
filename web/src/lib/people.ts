/**
 * How a member is named on screen.
 *
 * The API sends display names alongside ids on the reads that show one person
 * to another — a vouch row, a report in the queue, a moderation action. Before
 * that, every one of those places rendered the first eight characters of a uuid
 * and each did it slightly differently, so the town was populated by hex. These
 * two functions are the one rendering of a person's name, and the one rendering
 * of the fallback when there is no name to show.
 *
 * The fallback matters as much as the name. A member who has set no display
 * name comes back as the empty string on some responses and as an absent key on
 * others — the server cannot distinguish "no name set" from "not looked up" —
 * so a caller must fall back on anything falsy rather than on `undefined`
 * alone. Rendering the empty string leaves a row that names nobody at all.
 */

/** How many characters of a uuid stand in for a name. */
const SHORT_ID_LENGTH = 8;

/**
 * shortId labels somebody the interface knows only by id.
 *
 * The trailing ellipsis is not decoration: without it the eight characters read
 * as a whole identifier, and a moderator comparing one row against another has
 * no way to see that both are truncated.
 */
export function shortId(userId: string | null | undefined): string {
  const id = (userId ?? "").trim();
  // Nothing to truncate. "unknown" rather than an empty string or a bare
  // ellipsis, so a malformed row still reads as a sentence.
  if (!id) return "unknown";
  // The ellipsis is only honest when something was actually cut off.
  return id.length > SHORT_ID_LENGTH ? `${id.slice(0, SHORT_ID_LENGTH)}...` : id;
}

/**
 * personName renders somebody by name, falling back to their short id.
 *
 * Pass the display name straight from the DTO — absent, empty and
 * whitespace-only all take the fallback, which is what the three different
 * conventions on the wire require.
 */
export function personName(
  displayName: string | null | undefined,
  userId: string | null | undefined,
): string {
  const name = (displayName ?? "").trim();
  return name || shortId(userId);
}
