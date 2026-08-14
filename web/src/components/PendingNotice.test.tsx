import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { describe, expect, it } from "vitest";
import PendingNotice from "./PendingNotice";
import { PENDING_WELCOME } from "../lib/gating";

/**
 * A new arrival used to land on the feed with no explanation at all — the word
 * "pending" appeared nowhere in the interface, and the first thing anyone
 * learned about the rule was a composer that refused them. These pin the parts
 * of the greeting that make it an explanation rather than a rejection.
 */

function renderNotice() {
  return render(
    <MemoryRouter>
      <PendingNotice />
    </MemoryRouter>,
  );
}

describe("PendingNotice", () => {
  it("says what pending means and what ends it", () => {
    renderNotice();
    expect(screen.getByText(PENDING_WELCOME.meaning)).toBeInTheDocument();
    expect(screen.getByText(PENDING_WELCOME.next)).toBeInTheDocument();
  });

  // The one thing a pending member can act on, so it has to be reachable and
  // not merely mentioned.
  it("links to the profile they can go and complete", () => {
    renderNotice();
    expect(screen.getByRole("link", { name: PENDING_WELCOME.profileCta })).toHaveAttribute(
      "href",
      "/profile",
    );
  });

  it("is announced, and carries its own heading", () => {
    renderNotice();
    expect(screen.getByRole("status")).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: PENDING_WELCOME.title })).toBeInTheDocument();
  });
});
