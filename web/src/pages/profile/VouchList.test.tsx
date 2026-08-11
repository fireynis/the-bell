import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { User, Vouch } from "../../api/types";
import { REVOKE_PENALTY } from "../../lib/vouch";
import VouchList from "./VouchList";

/**
 * VouchService.Revoke lets the voucher or any moderator revoke, but the control
 * lived only on the voucher's own view — so a moderator had to open every
 * vouchee's profile one at a time. These pin the affordance and, more
 * importantly, the cost it quotes: the -3 trust penalty is charged only when
 * the revoker is the voucher, so quoting it to a moderator would be a threat
 * the server never carries out.
 */

const VOUCHER = "voucher-1";
const VOUCHEE = "vouchee-1";

function vouch(overrides: Partial<Vouch> = {}): Vouch {
  return {
    id: "vouch-1",
    voucher_id: VOUCHER,
    vouchee_id: VOUCHEE,
    status: "active",
    created_at: "2026-03-01T12:00:00Z",
    ...overrides,
  };
}

function user(overrides: Partial<User> = {}): User {
  return {
    id: VOUCHER,
    display_name: "Ada Lovelace",
    bio: "",
    avatar_url: "",
    trust_score: 60,
    role: "member",
    is_active: true,
    joined_at: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

/** Renders the "received" side of a profile, where the row's voucher is someone else. */
function renderList(viewer: User | null, vouches: Vouch[] = [vouch()], onRevoked = vi.fn()) {
  return render(
    <MemoryRouter>
      <VouchList
        direction="received"
        vouches={vouches}
        viewer={viewer}
        ownerName="Bob Vouchee"
        onRevoked={onRevoked}
      />
    </MemoryRouter>,
  );
}

const revokeButton = () => screen.queryByRole("button", { name: "Revoke" });

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("VouchList revoke control", () => {
  it("offers revoke to a moderator on somebody else's vouch", () => {
    renderList(user({ id: "mod-1", role: "moderator" }));
    expect(revokeButton()).toBeInTheDocument();
  });

  it("offers revoke to the member who gave the vouch", () => {
    renderList(user({ id: VOUCHER }));
    expect(revokeButton()).toBeInTheDocument();
  });

  it.each([
    ["an ordinary member who is not the voucher", user({ id: "other-1", role: "member" })],
    ["a suspended moderator", user({ id: "mod-1", role: "moderator", is_active: false })],
    ["nobody signed in", null],
  ])("offers no revoke to %s", (_label, viewer) => {
    renderList(viewer);
    expect(revokeButton()).not.toBeInTheDocument();
  });

  it("offers no revoke on a vouch that is already revoked", () => {
    renderList(user({ id: "mod-1", role: "moderator" }), [vouch({ status: "revoked" })]);
    expect(revokeButton()).not.toBeInTheDocument();
  });
});

describe("VouchList revoke warning", () => {
  // The penalty is charged only when the revoker is the voucher, so quoting it
  // to a moderator would promise a cost the server never applies.
  it("does not tell a moderator the revocation costs them trust", () => {
    renderList(user({ id: "mod-1", role: "moderator" }));

    fireEvent.click(revokeButton()!);

    const dialog = screen.getByRole("dialog");
    expect(dialog).not.toHaveTextContent("costs you");
    expect(dialog).toHaveTextContent("does not cost you any trust");
  });

  it("tells the voucher revoking their own vouch what it will cost them", () => {
    renderList(user({ id: VOUCHER }));

    fireEvent.click(revokeButton()!);

    expect(screen.getByRole("dialog")).toHaveTextContent(`costs you ${REVOKE_PENALTY} trust`);
  });
});

describe("VouchList revoking", () => {
  it("sends the revocation and tells the page to reload", async () => {
    const fetchMock = vi.fn(() =>
      Promise.resolve({ ok: true, status: 204, json: () => Promise.resolve(null) } as Response),
    );
    vi.stubGlobal("fetch", fetchMock);
    const onRevoked = vi.fn();

    renderList(user({ id: "mod-1", role: "moderator" }), [vouch()], onRevoked);
    fireEvent.click(revokeButton()!);
    fireEvent.click(screen.getByRole("button", { name: "Revoke vouch" }));

    await waitFor(() => expect(onRevoked).toHaveBeenCalledTimes(1));
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/v1/vouches/vouch-1",
      expect.objectContaining({ method: "DELETE" }),
    );
  });

  // The row is still there on the server, so the list must not act as though
  // the vouch is gone.
  it("keeps the dialog open and shows why when the server refuses", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(() =>
        Promise.resolve({
          ok: false,
          status: 403,
          json: () => Promise.resolve({ error: "forbidden" }),
        } as Response),
      ),
    );
    const onRevoked = vi.fn();

    renderList(user({ id: "mod-1", role: "moderator" }), [vouch()], onRevoked);
    fireEvent.click(revokeButton()!);
    fireEvent.click(screen.getByRole("button", { name: "Revoke vouch" }));

    await waitFor(() => expect(screen.getByRole("dialog")).toHaveTextContent("forbidden"));
    expect(onRevoked).not.toHaveBeenCalled();
  });
});
