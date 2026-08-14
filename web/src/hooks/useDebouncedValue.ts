import { useEffect, useState } from "react";

/**
 * How long the directory waits before searching. Long enough that typing a name
 * is one request rather than one per letter, short enough that it still feels
 * like the list is answering as you type.
 */
export const SEARCH_DEBOUNCE_MS = 250;

/**
 * useDebouncedValue follows a value, but only after it has held still.
 *
 * The search box drives a request, and a request per keystroke is both a
 * needless load on the server and a race the client cannot win: eight letters
 * typed quickly means eight overlapping fetches, any of which may land last and
 * leave the list showing results for a prefix nobody is looking at any more.
 * Waiting for the typing to pause means one request, for the query the member
 * actually stopped on.
 *
 * The first value is returned as-is rather than delayed, so a page whose search
 * starts empty loads its first page immediately instead of after a pause that
 * looks like a stall.
 */
export function useDebouncedValue<T>(value: T, delayMs: number = SEARCH_DEBOUNCE_MS): T {
  const [settled, setSettled] = useState(value);

  useEffect(() => {
    const timer = setTimeout(() => setSettled(value), delayMs);
    // Every new value cancels the timer the previous one started, which is what
    // makes a burst of keystrokes settle once rather than once each.
    return () => clearTimeout(timer);
  }, [value, delayMs]);

  return settled;
}
