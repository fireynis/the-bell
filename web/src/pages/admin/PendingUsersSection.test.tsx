import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { describe, expect, it, vi } from "vitest";
import type { User } from "../../api/types";
import { NO_RESIDENCY_CLAIM } from "../../lib/residency";
import PendingUsersSection from "./PendingUsersSection";

/**
 * The queue is where a council member decides whether they recognise a stranger,
 * and the residency claim is the only thing on the row that helps them. These
 * pin that it reads as a claim rather than as a checked fact, and that a member
 * who gave none is not left looking like a row that failed to load.
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

function renderSection(users: User[]) {
  return render(
    <MemoryRouter>
      <PendingUsersSection users={users} onApprove={vi.fn()} approving={null} />
    </MemoryRouter>,
  );
}

describe("PendingUsersSection", () => {
  it("shows the claim as something the newcomer says, not as their address", () => {
    renderSection([pendingUser({ residency_claim: "the old mill road" })]);

    expect(screen.getByText("Says they're at or near the old mill road")).toBeInTheDocument();
  });

  it("says quietly that no address was given rather than leaving a gap", () => {
    renderSection([pendingUser({ residency_claim: "" })]);

    expect(screen.getByText(NO_RESIDENCY_CLAIM)).toBeInTheDocument();
  });

  // The field is absent entirely on a build whose server does not send it, which
  // must read the same as a member who chose not to answer.
  it("says the same when the row carries no claim at all", () => {
    renderSection([pendingUser()]);

    expect(screen.getByText(NO_RESIDENCY_CLAIM)).toBeInTheDocument();
  });

  it("still names and offers to approve each pending user", () => {
    renderSection([pendingUser({ residency_claim: "by the school" })]);

    expect(screen.getByRole("link", { name: "Ada Lovelace" })).toHaveAttribute(
      "href",
      "/profile/0193a7b2-aaaa-7000-8000-000000000000",
    );
    expect(screen.getByRole("button", { name: "Approve" })).toBeEnabled();
  });

  it("says there is nobody waiting when the queue is empty", () => {
    renderSection([]);

    expect(screen.getByText("No pending users at this time.")).toBeInTheDocument();
  });
});
