import { render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import { postApi } from "../api/client";
import { AuthProvider } from "../context/AuthContext";
import { BOTTOM_NAV_ITEMS, NAV_ITEMS } from "../lib/nav";
import { UNVERIFIED_EMAIL_NOTICE, VERIFICATION_PATH } from "../lib/verification";
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

function renderLayout(props: { wide?: boolean } = {}) {
  stubApi();
  return render(
    <MemoryRouter>
      <AuthProvider>
        <AppLayout {...props} />
      </AuthProvider>
    </MemoryRouter>,
  );
}

/** The single centred column every page is mounted inside. */
function contentColumn(container: HTMLElement) {
  const column = container.querySelector("main > div");
  expect(column).not.toBeNull();
  return column as HTMLElement;
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

/**
 * The layout owns the one horizontal gutter and the one measure. Pages used to
 * add their own `mx-auto max-w-2xl p-4` inside it, doubling the padding and
 * narrowing the column twice; Town Hall's four-across stat row was the visible
 * cost, crushed into a 600px reading column.
 */
/**
 * The unverified-email banner.
 *
 * The guard skips GET /v1/me, so this member is signed in and looks entirely
 * ordinary — and every other call is refused with a message no page used to
 * recognize. The banner belongs here rather than on any one page because the
 * refusal belongs to the session: whichever page they open, the same thing is
 * in the way, and it is not that page's failure to explain.
 */
describe("AppLayout unverified email banner", () => {
  const notice = () => screen.queryByText(UNVERIFIED_EMAIL_NOTICE, { exact: false });

  it("says nothing while nothing has been refused", async () => {
    renderLayout();

    await waitFor(() => expect(screen.getByRole("banner")).toBeInTheDocument());
    expect(notice()).toBeNull();
  });

  it("appears once the API refuses a call for want of a verified address", async () => {
    renderLayout();
    await waitFor(() => expect(screen.getByRole("banner")).toBeInTheDocument());

    // Any call at all: the layout learns from the client rather than from the
    // page that made it.
    vi.stubGlobal(
      "fetch",
      vi.fn(() =>
        Promise.resolve({
          ok: false,
          status: 403,
          json: () => Promise.resolve({ error: "email not verified" }),
        } as unknown as Response),
      ),
    );
    await postApi.create({ body: "hello" }).catch(() => undefined);

    expect(await screen.findByText(UNVERIFIED_EMAIL_NOTICE, { exact: false })).toBeInTheDocument();
  });

  it("offers the way out of it", async () => {
    renderLayout();
    await waitFor(() => expect(screen.getByRole("banner")).toBeInTheDocument());

    vi.stubGlobal(
      "fetch",
      vi.fn(() =>
        Promise.resolve({
          ok: false,
          status: 403,
          json: () => Promise.resolve({ error: "email not verified" }),
        } as unknown as Response),
      ),
    );
    await postApi.create({ body: "hello" }).catch(() => undefined);

    const link = await screen.findByRole("link", { name: /verify your email/i });
    expect(link).toHaveAttribute("href", VERIFICATION_PATH);
  });

  // An ordinary refusal is the page's business, not the whole session's.
  it("stays quiet for a 403 that means something else", async () => {
    renderLayout();
    await waitFor(() => expect(screen.getByRole("banner")).toBeInTheDocument());

    vi.stubGlobal(
      "fetch",
      vi.fn(() =>
        Promise.resolve({
          ok: false,
          status: 403,
          json: () => Promise.resolve({ error: "account suspended" }),
        } as unknown as Response),
      ),
    );
    await postApi.create({ body: "hello" }).catch(() => undefined);

    expect(notice()).toBeNull();
  });
});

describe("AppLayout content column", () => {
  it("holds pages to the reading measure by default", () => {
    const { container } = renderLayout();
    expect(contentColumn(container).style.maxWidth).toBe("var(--content-max-width)");
  });

  it("widens for a page that is a dashboard rather than a column of posts", () => {
    const { container } = renderLayout({ wide: true });
    expect(contentColumn(container).style.maxWidth).toBe("var(--content-max-width-wide)");
  });

  it("supplies the horizontal gutter itself, so pages do not add a second one", () => {
    const { container } = renderLayout();
    expect(contentColumn(container).className).toContain("px-4");
  });
});
