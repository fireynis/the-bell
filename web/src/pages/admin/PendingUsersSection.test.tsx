import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { describe, expect, it } from "vitest";
import type { User } from "../../api/types";
import { APPROVALS_PREVIEW_SIZE } from "../../lib/approvals";
import { NO_RESIDENCY_CLAIM } from "../../lib/residency";
import PendingUsersSection from "./PendingUsersSection";

/**
 * The dashboard's preview of the approval queue. It exists to say how many
 * neighbours are waiting and to get somebody to the queue; the deciding happens
 * on /admin/approvals. These pin the boundary in both directions — the preview
 * stays short and carries no claim, and it does not disappear the queue.
 */

function pendingUser(overrides: Partial<User> = {}): User {
  return {
    id: "0193a7b2-aaaa-7000-8000-000000000000",
    display_name: "Ada Lovelace",
    bio: "",
    avatar_url: "",
    trust_score: 0,
    role: "pending",
    is_active: true,
    joined_at: "2026-03-01T12:00:00Z",
    ...overrides,
  };
}

function renderSection(users: User[], total = users.length) {
  return render(
    <MemoryRouter>
      <PendingUsersSection users={users} total={total} />
    </MemoryRouter>,
  );
}

describe("Town Hall approvals preview", () => {
  it("counts the neighbours waiting, in the words a town uses", () => {
    renderSection([pendingUser()], 7);

    expect(screen.getByText("7 neighbours waiting")).toBeInTheDocument();
  });

  it("says one neighbour rather than 1 neighbours", () => {
    renderSection([pendingUser()], 1);

    expect(screen.getByText("1 neighbour waiting")).toBeInTheDocument();
  });

  it("names the longest-waiting applicant and links to their profile", () => {
    renderSection([pendingUser()], 1);

    expect(screen.getByRole("link", { name: "Ada Lovelace" })).toHaveAttribute(
      "href",
      "/profile/0193a7b2-aaaa-7000-8000-000000000000",
    );
  });

  it("offers a way through to the whole queue", () => {
    renderSection([pendingUser()], 12);

    expect(screen.getByRole("link", { name: "Review all" })).toHaveAttribute(
      "href",
      "/admin/approvals",
    );
  });

  // The wall this page was rewritten to stop rendering. Even handed more, the
  // preview stays a preview.
  it("previews only the longest-waiting few, however many it is given", () => {
    const many = Array.from({ length: 10 }, (_, i) =>
      pendingUser({ id: `user-${i}`, display_name: `Waiting ${i}` }),
    );

    renderSection(many, 50);

    expect(screen.getAllByRole("listitem")).toHaveLength(APPROVALS_PREVIEW_SIZE);
    expect(screen.getByText("Waiting 0")).toBeInTheDocument();
    expect(screen.queryByText("Waiting 5")).not.toBeInTheDocument();
  });

  // The claim is the most sensitive thing a resident tells the town and belongs
  // to the reviewing screen alone. A dashboard is not a review.
  it("does not show the residency claim", () => {
    renderSection([pendingUser({ residency_claim: "the old mill road" })], 1);

    expect(screen.queryByText(/old mill road/)).not.toBeInTheDocument();
    expect(screen.queryByText(NO_RESIDENCY_CLAIM)).not.toBeInTheDocument();
  });

  // Approving is a judgement about a stranger and wants the queue's context.
  it("offers no approve button", () => {
    renderSection([pendingUser()], 1);

    expect(screen.queryByRole("button", { name: /approve/i })).not.toBeInTheDocument();
  });

  it("says the town is caught up when nobody is waiting", () => {
    renderSection([], 0);

    expect(
      screen.getByText("Nobody is waiting — the town is all caught up."),
    ).toBeInTheDocument();
  });
});
