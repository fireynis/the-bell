import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import ReactionButton from "./ReactionButton";

/**
 * The visible label is an emoji and a bare number, so everything a screen
 * reader gets comes from the accessible name and aria-pressed. aria-pressed is
 * also what CSS colours the pill from, which is the point: the state a reader
 * hears and the state they see are the same attribute.
 */

function stubApi() {
  vi.stubGlobal(
    "fetch",
    vi.fn(() =>
      Promise.resolve({
        ok: true,
        status: 200,
        json: () => Promise.resolve({}),
      } as unknown as Response),
    ),
  );
}

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("ReactionButton", () => {
  it("says what it is and how many, not '🔔 3'", () => {
    render(<ReactionButton postId="p1" type="bell" count={3} active={false} />);
    expect(screen.getByRole("button", { name: "Bell, 3 reactions" })).toBeInTheDocument();
  });

  it("reports whether the reader has already reacted", () => {
    render(<ReactionButton postId="p1" type="heart" count={1} active />);
    expect(screen.getByRole("button", { name: "Heart, 1 reaction" }))
      .toHaveAttribute("aria-pressed", "true");
  });

  it("is unpressed when the reader has not reacted", () => {
    render(<ReactionButton postId="p1" type="heart" count={1} active={false} />);
    expect(screen.getByRole("button", { name: "Heart, 1 reaction" }))
      .toHaveAttribute("aria-pressed", "false");
  });

  // The optimistic update has to reach the announcement too, or a screen reader
  // is told the old count until the next feed load.
  it("updates the name and the pressed state on the optimistic toggle", () => {
    stubApi();
    render(<ReactionButton postId="p1" type="bell" count={3} active={false} />);

    fireEvent.click(screen.getByRole("button", { name: "Bell, 3 reactions" }));

    const button = screen.getByRole("button", { name: "Bell, 4 reactions" });
    expect(button).toHaveAttribute("aria-pressed", "true");
  });

  // Both are decoration on a control the name already describes; announcing
  // them would repeat the count and read the emoji out as a word.
  it("hides the emoji and the number from assistive tech", () => {
    render(<ReactionButton postId="p1" type="bell" count={3} active={false} />);
    const spans = screen
      .getByRole("button", { name: "Bell, 3 reactions" })
      .querySelectorAll("span");

    expect(spans).toHaveLength(2);
    spans.forEach((span) => expect(span).toHaveAttribute("aria-hidden", "true"));
  });

  // The pill's colours are driven from aria-pressed in CSS, so the class has to
  // be there for the attribute to mean anything visually.
  it("draws itself from the class whose rules key off aria-pressed", () => {
    render(<ReactionButton postId="p1" type="bell" count={0} active={false} />);
    expect(screen.getByRole("button", { name: "Bell, no reactions" }).className)
      .toContain("reaction-pill");
  });
});
