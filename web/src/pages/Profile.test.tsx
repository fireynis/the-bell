import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { OwnModerationEntry } from "../api/types";
import { AuthProvider } from "../context/AuthContext";
import Profile from "./Profile";

/**
 * The profile page's "My history" tab: a member's own moderation record.
 *
 * What matters here is where it appears and when it is fetched. The wording of
 * an entry is pinned in lib/ownHistory.test.ts, and the rendering in
 * components/OwnModerationHistory.test.tsx.
 */

const VIEWER_ID = "0193a7b2-aaaa-7000-8000-000000000001";
const NEIGHBOUR_ID = "0193a7b2-bbbb-7000-8000-000000000002";

const answer = (body: unknown, ok = true, status = 200) =>
  Promise.resolve({ ok, status, json: () => Promise.resolve(body) } as unknown as Response);

function profileBody(id: string, name: string) {
  return {
    id,
    display_name: name,
    bio: "",
    avatar_url: "",
    trust_score: 60,
    role: "member",
    is_active: true,
    joined_at: "2026-01-01T00:00:00Z",
  };
}

/**
 * Stubs every request a rendered profile makes, and records the ones aimed at
 * the moderation history so a test can assert it was not asked for.
 */
function stubApi(history: OwnModerationEntry[] = []) {
  const historyCalls: string[] = [];

  vi.stubGlobal(
    "fetch",
    vi.fn((url: string) => {
      if (url.startsWith("/.ory")) return answer({ id: "session-1", active: true });
      // Checked before the bare /users/me below, which is a prefix of it.
      if (url.startsWith("/api/v1/users/me/moderation-history")) {
        historyCalls.push(url);
        return answer({ actions: history });
      }
      if (url.startsWith("/api/v1/me") || url.startsWith("/api/v1/users/me")) {
        return answer(profileBody(VIEWER_ID, "Ada"));
      }
      if (url.includes("/posts")) return answer({ posts: [] });
      if (url.includes("/vouches")) return answer({ received: [], given: [] });
      if (url.startsWith("/api/v1/users/")) {
        return answer(profileBody(NEIGHBOUR_ID, "Grace"));
      }
      return answer({});
    }),
  );

  return historyCalls;
}

/** Renders the own-profile route ("/profile") or a neighbour's. */
function renderProfile(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <AuthProvider>
        <Routes>
          <Route path="/profile" element={<Profile />} />
          <Route path="/profile/:userId" element={<Profile />} />
        </Routes>
      </AuthProvider>
    </MemoryRouter>,
  );
}

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("Profile — my moderation history", () => {
  it("offers the tab on the member's own profile", async () => {
    stubApi();

    renderProfile("/profile");

    expect(await screen.findByRole("button", { name: "My history" })).toBeTruthy();
  });

  it("does not offer it on a neighbour's profile", async () => {
    // The endpoint answers only about the caller, so there is nothing this tab
    // could show on somebody else's page.
    stubApi();

    renderProfile(`/profile/${NEIGHBOUR_ID}`);

    await screen.findByRole("button", { name: /^Posts/ });
    expect(screen.queryByRole("button", { name: "My history" })).toBeNull();
  });

  it("does not ask the server for the history until the tab is opened", async () => {
    // Most members have nothing here. Loading it with every profile view would
    // buy a request per page load to render one sentence nobody asked for.
    const historyCalls = stubApi();

    renderProfile("/profile");

    await screen.findByRole("button", { name: "My history" });
    expect(historyCalls).toHaveLength(0);

    fireEvent.click(screen.getByRole("button", { name: "My history" }));

    await waitFor(() => expect(historyCalls).toHaveLength(1));
  });

  it("shows the record once the tab is opened", async () => {
    stubApi([
      {
        id: "act-1",
        action: "warn",
        severity: 1,
        reason: "posting the same thing repeatedly",
        created_at: "2026-03-01T12:00:00Z",
        penalty: { amount: 5, decays_at: "2026-05-30T12:00:00Z" },
      },
    ]);

    renderProfile("/profile");

    fireEvent.click(await screen.findByRole("button", { name: "My history" }));

    expect(await screen.findByText("You were warned — minor")).toBeTruthy();
    expect(screen.getByText("posting the same thing repeatedly")).toBeTruthy();
  });

  it("reassures a member whose record is clean", async () => {
    stubApi([]);

    renderProfile("/profile");

    fireEvent.click(await screen.findByRole("button", { name: "My history" }));

    expect(
      await screen.findByText("Nothing here — and that's how it stays for most people."),
    ).toBeTruthy();
  });
});
