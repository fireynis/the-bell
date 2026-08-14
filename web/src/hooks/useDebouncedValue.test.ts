import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { SEARCH_DEBOUNCE_MS, useDebouncedValue } from "./useDebouncedValue";

/**
 * A request per keystroke is not only wasteful, it is a race the client cannot
 * win: the response for "al" can land after the response for "alice" and leave
 * the list showing results for a prefix nobody is looking at. These pin that a
 * burst settles once, on the last value.
 */

beforeEach(() => {
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
});

/** Advances the clock inside act, so React flushes the state the timer set. */
function elapse(ms: number) {
  act(() => {
    vi.advanceTimersByTime(ms);
  });
}

describe("useDebouncedValue", () => {
  // A page whose search starts empty must load immediately; a pause before the
  // first fetch reads as a stall.
  it("returns the first value without waiting", () => {
    const { result } = renderHook(() => useDebouncedValue("", SEARCH_DEBOUNCE_MS));
    expect(result.current).toBe("");
  });

  it("holds the previous value until the delay has passed", () => {
    const { result, rerender } = renderHook(({ v }) => useDebouncedValue(v, 250), {
      initialProps: { v: "a" },
    });

    rerender({ v: "ali" });
    elapse(249);
    expect(result.current).toBe("a");

    elapse(1);
    expect(result.current).toBe("ali");
  });

  it("settles once on the last value of a burst of keystrokes", () => {
    const { result, rerender } = renderHook(({ v }) => useDebouncedValue(v, 250), {
      initialProps: { v: "" },
    });

    for (const typed of ["a", "al", "ali", "alic", "alice"]) {
      rerender({ v: typed });
      elapse(100);
    }

    // Every keystroke cancelled the timer before it fired, so nothing has
    // settled yet — and when it does it is the whole word, not a prefix.
    expect(result.current).toBe("");
    elapse(250);
    expect(result.current).toBe("alice");
  });

  it("follows the value back to empty when the box is cleared", () => {
    const { result, rerender } = renderHook(({ v }) => useDebouncedValue(v, 250), {
      initialProps: { v: "alice" },
    });

    rerender({ v: "" });
    elapse(250);
    expect(result.current).toBe("");
  });

  it("cancels the pending timer when the page unmounts", () => {
    const { rerender, unmount } = renderHook(({ v }) => useDebouncedValue(v, 250), {
      initialProps: { v: "a" },
    });

    rerender({ v: "alice" });
    unmount();

    // A timer surviving the unmount would set state on a gone component.
    expect(vi.getTimerCount()).toBe(0);
  });

  it("debounces values of any type, not only search text", () => {
    const { result, rerender } = renderHook(({ v }) => useDebouncedValue(v, 250), {
      initialProps: { v: 1 },
    });

    rerender({ v: 2 });
    elapse(250);
    expect(result.current).toBe(2);
  });
});
