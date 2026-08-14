import { render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import { AuthProvider } from "../context/AuthContext";
import { BOTTOM_NAV_ITEMS, NAV_ITEMS } from "../lib/nav";
import AppLayout from "./AppLayout";

/**
 * Settings is not in the bottom bar — six items did not fit at 360px — and the
 * sidebar that carries it everywhere else is `hidden lg:flex`. So this header
 * is the only route a phone has to a member's own account, and the two facts
 * only make sense together: whichever of them is changed, the other has to be
 * looked at, or mobile members are left with no way in.
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

function renderLayout() {
  stubApi();
  return render(
    <MemoryRouter>
      <AuthProvider>
        <AppLayout />
      </AuthProvider>
    </MemoryRouter>,
  );
}

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

/**
 * Scoped to the header, because the sidebar renders a Settings link too and
 * jsdom applies no CSS — under test both are in the document, and only the
 * `lg:` classes decide which one a reader can actually see.
 */
const header = () => within(screen.getByRole("banner"));

describe("AppLayout mobile header", () => {
  it("offers a way to reach settings", async () => {
    renderLayout();

    await waitFor(() =>
      expect(header().getByRole("link", { name: NAV_ITEMS.settings.label })).toHaveAttribute(
        "href",
        NAV_ITEMS.settings.path,
      ),
    );
  });

  // Belt and braces on the arrangement itself rather than on either half of it:
  // settings has to be reachable from somewhere that is not the desktop-only
  // sidebar, and this header is that somewhere.
  it("is the mobile route to settings precisely because the bottom bar is not", () => {
    expect(BOTTOM_NAV_ITEMS).not.toContain(NAV_ITEMS.settings);
    renderLayout();
    expect(header().getByRole("link", { name: NAV_ITEMS.settings.label })).toBeInTheDocument();
  });
});
