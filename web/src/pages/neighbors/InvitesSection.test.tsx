import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Invite } from "../../api/types";
import { INVITES_SECTION } from "../../lib/invite";
import InvitesSection from "./InvitesSection";

/**
 * The sender's record of who they have invited.
 *
 * It is collapsed by default and says how many are still out, because the page
 * it sits on is for finding a neighbour rather than for administering a list —
 * and because an accumulating history of accepted and expired invitations would
 * otherwise push the directory off the screen.
 *
 * Only open invitations can be withdrawn. Offering it on an accepted one would
 * suggest the vouch behind it comes back too, and it does not.
 */

function invite(overrides: Partial<Invite> = {}): Invite {
  return {
    id: "i1",
    email: "newcomer@example.com",
    note: "",
    status: "open",
    created_at: "2026-03-01T00:00:00Z",
    expires_at: new Date(Date.now() + 5 * 86_400_000).toISOString(),
    ...overrides,
  };
}

interface StubOptions {
  invites?: Invite[];
  /** Answers the listing with a 500. */
  failList?: boolean;
  /** Answers the revoke with this status instead of 204. */
  failRevoke?: number;
}

/** Stubs the invite listing and the revoke, recording every request made. */
function stubApi(options: StubOptions = {}) {
  const calls: Array<{ url: string; method: string }> = [];
  let listed = options.invites ?? [invite()];

  vi.stubGlobal(
    "fetch",
    vi.fn((url: string, init?: RequestInit) => {
      const method = init?.method ?? "GET";
      calls.push({ url, method });

      if (method === "DELETE") {
        if (options.failRevoke) {
          return Promise.resolve({
            ok: false,
            status: options.failRevoke,
            json: () => Promise.resolve({ error: "nope" }),
          } as unknown as Response);
        }
        listed = listed.map((i) => (i.status === "open" ? { ...i, status: "revoked" } : i));
        return Promise.resolve({ ok: true, status: 204 } as unknown as Response);
      }

      if (options.failList) {
        return Promise.resolve({
          ok: false,
          status: 500,
          json: () => Promise.resolve({ error: "internal error" }),
        } as unknown as Response);
      }
      return Promise.resolve({
        ok: true,
        status: 200,
        json: () => Promise.resolve({ invites: listed }),
      } as unknown as Response);
    }),
  );

  return calls;
}

const toggle = () => screen.getByRole("button", { name: /Your invitations/ });
const expand = async () => {
  fireEvent.click(toggle());
  await waitFor(() => expect(toggle()).toHaveAttribute("aria-expanded", "true"));
};

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("InvitesSection collapsed", () => {
  it("starts collapsed, with nothing from the list on screen", async () => {
    stubApi();

    render(<InvitesSection reloadToken={0} />);

    await waitFor(() => expect(toggle()).toHaveAttribute("aria-expanded", "false"));
    expect(screen.queryByText("newcomer@example.com")).not.toBeInTheDocument();
  });

  // Otherwise the only way to learn there is nothing worth opening is to open it.
  it("says how many are still waiting without being opened", async () => {
    stubApi({ invites: [invite(), invite({ id: "i2", status: "accepted" })] });

    render(<InvitesSection reloadToken={0} />);

    expect(await screen.findByText(/1 still waiting/)).toBeInTheDocument();
  });

  it("counts nothing when every invitation has been answered", async () => {
    stubApi({ invites: [invite({ status: "accepted" })] });

    render(<InvitesSection reloadToken={0} />);

    await waitFor(() => expect(toggle()).toBeInTheDocument());
    expect(screen.queryByText(/still waiting/)).not.toBeInTheDocument();
  });
});

describe("InvitesSection listing", () => {
  it("shows an open invitation counting down to its expiry", async () => {
    stubApi();

    render(<InvitesSection reloadToken={0} />);
    await expand();

    expect(screen.getByText("newcomer@example.com")).toBeInTheDocument();
    expect(screen.getByText("Expires in 5 days")).toBeInTheDocument();
  });

  it("names who an accepted invitation became", async () => {
    stubApi({
      invites: [
        invite({ status: "accepted", consumed_by_display_name: "Grace Hopper" }),
      ],
    });

    render(<InvitesSection reloadToken={0} />);
    await expand();

    expect(screen.getByText("Accepted — they joined as Grace Hopper")).toBeInTheDocument();
  });

  it.each([
    ["expired", /Expired/],
    ["revoked", /withdrew/],
  ] as const)("says where a %s invitation ended up", async (status, wording) => {
    stubApi({ invites: [invite({ status })] });

    render(<InvitesSection reloadToken={0} />);
    await expand();

    expect(screen.getByText(wording)).toBeInTheDocument();
  });

  // The note is the only part the sender wrote, and how they tell one
  // invitation from another a week later.
  it("shows the sender their own note back", async () => {
    stubApi({ invites: [invite({ note: "We met at the market." })] });

    render(<InvitesSection reloadToken={0} />);
    await expand();

    expect(screen.getByText(/We met at the market\./)).toBeInTheDocument();
  });

  it("invites the sender to think of somebody when the list is empty", async () => {
    stubApi({ invites: [] });

    render(<InvitesSection reloadToken={0} />);
    await expand();

    expect(screen.getByText(INVITES_SECTION.empty)).toBeInTheDocument();
  });

  it("says so, and offers a retry, when the list cannot be read", async () => {
    stubApi({ failList: true });

    render(<InvitesSection reloadToken={0} />);
    await expand();

    expect(await screen.findByText(INVITES_SECTION.loadError)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Retry" })).toBeInTheDocument();
  });

  // The page bumps the token when the dialog has just created one.
  it("re-reads the list when the page says something was sent", async () => {
    const calls = stubApi();

    const { rerender } = render(<InvitesSection reloadToken={0} />);
    await waitFor(() => expect(calls.filter((c) => c.method === "GET")).toHaveLength(1));

    rerender(<InvitesSection reloadToken={1} />);

    await waitFor(() => expect(calls.filter((c) => c.method === "GET")).toHaveLength(2));
  });
});

describe("InvitesSection revoking", () => {
  const revokeButton = () =>
    screen.getByRole("button", { name: /Revoke the invitation to newcomer@example.com/ });

  it("offers to withdraw an open invitation, behind a confirmation", async () => {
    stubApi();

    render(<InvitesSection reloadToken={0} />);
    await expand();
    fireEvent.click(revokeButton());

    expect(screen.getByRole("dialog", { name: INVITES_SECTION.revokeTitle })).toBeInTheDocument();
    // Names the address, so nobody withdraws the wrong one.
    expect(screen.getByText(/newcomer@example\.com will stop working/)).toBeInTheDocument();
  });

  it.each(["accepted", "expired", "revoked"] as const)(
    "does not offer to withdraw a %s invitation",
    async (status) => {
      stubApi({ invites: [invite({ status })] });

      render(<InvitesSection reloadToken={0} />);
      await expand();

      expect(screen.queryByRole("button", { name: /Revoke the invitation/ })).not.toBeInTheDocument();
    },
  );

  it("leaves the invitation alone when the confirmation is dismissed", async () => {
    const calls = stubApi();

    render(<InvitesSection reloadToken={0} />);
    await expand();
    fireEvent.click(revokeButton());
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));

    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(calls.some((c) => c.method === "DELETE")).toBe(false);
  });

  it("withdraws it once confirmed, and re-reads the list", async () => {
    const calls = stubApi();

    render(<InvitesSection reloadToken={0} />);
    await expand();
    fireEvent.click(revokeButton());
    fireEvent.click(screen.getByRole("button", { name: INVITES_SECTION.revoke }));

    await waitFor(() => expect(calls.some((c) => c.method === "DELETE")).toBe(true));
    expect(await screen.findByText(/withdrew/)).toBeInTheDocument();
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  // The dialog stays open so the failure is attached to the thing that failed.
  it("keeps the confirmation open, saying why, when the withdrawal fails", async () => {
    stubApi({ failRevoke: 404 });

    render(<InvitesSection reloadToken={0} />);
    await expand();
    fireEvent.click(revokeButton());
    fireEvent.click(screen.getByRole("button", { name: INVITES_SECTION.revoke }));

    expect(await screen.findByText(/no longer there/)).toBeInTheDocument();
    expect(screen.getByRole("dialog", { name: INVITES_SECTION.revokeTitle })).toBeInTheDocument();
  });
});
