import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import PendingNotice from "./PendingNotice";
import { AuthProvider } from "../context/AuthContext";
import { PENDING_WELCOME, RESIDENCY_PROMPT } from "../lib/gating";

/**
 * A new arrival used to land on the feed with no explanation at all — the word
 * "pending" appeared nowhere in the interface, and the first thing anyone
 * learned about the rule was a composer that refused them. These pin the parts
 * of the greeting that make it an explanation rather than a rejection.
 */

/**
 * The notice carries the residency prompt, which reads the signed-in member, so
 * it needs a session around it. Answering every request with an empty body is
 * enough: the prompt starts blank for a member whose profile carries no claim,
 * which is every member until they answer it.
 */
function renderNotice() {
  vi.stubGlobal(
    "fetch",
    vi.fn(() =>
      Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve({}) } as Response),
    ),
  );

  return render(
    <MemoryRouter>
      <AuthProvider>
        <PendingNotice />
      </AuthProvider>
    </MemoryRouter>,
  );
}

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

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

  // The welcome says a neighbour has to recognise them; this is where they get
  // to help with that, so the two belong on screen together.
  it("asks where in town they are, alongside the welcome", () => {
    renderNotice();
    expect(screen.getByLabelText(RESIDENCY_PROMPT.label)).toBeInTheDocument();
    expect(screen.getByText(RESIDENCY_PROMPT.help)).toBeInTheDocument();
  });
});
