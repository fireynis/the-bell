import { render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import Settings from "./Settings";

/**
 * Signing out used to live only in the desktop sidebar pill, which is
 * `hidden lg:flex`. Below lg there was no way to leave a session at all — so
 * these pin that this page carries one, and that it is the same logout the
 * sidebar runs rather than a second implementation that could drift.
 */

const logout = vi.fn(() => Promise.resolve());
const navigate = vi.fn();

vi.mock("../../context/AuthContext.tsx", () => ({
  useAuth: () => ({
    session: null,
    user: null,
    profileError: false,
    loading: false,
    refreshSession: vi.fn(),
    logout,
  }),
}));

vi.mock("../../hooks/useFlow.ts", () => ({
  useFlow: () => ({
    flow: null,
    error: null,
    submitting: false,
    submit: vi.fn(),
    setFlow: vi.fn(),
  }),
}));

vi.mock("react-router", async () => {
  const actual = await vi.importActual<typeof import("react-router")>("react-router");
  return { ...actual, useNavigate: () => navigate };
});

function renderSettings() {
  return render(
    <MemoryRouter>
      <Settings />
    </MemoryRouter>,
  );
}

afterEach(() => {
  vi.clearAllMocks();
});

describe("Settings sign-out", () => {
  it("offers a way to sign out", () => {
    renderSettings();
    expect(screen.getByRole("button", { name: "Sign out" })).toBeInTheDocument();
  });

  it("ends the session through the same logout the sidebar uses", async () => {
    renderSettings();

    screen.getByRole("button", { name: "Sign out" }).click();

    await waitFor(() => expect(logout).toHaveBeenCalledTimes(1));
  });

  // The sidebar sits in the app shell, which re-renders itself out of an
  // authenticated state. This page would otherwise sit there looking signed in.
  it("leaves for the login page once the session is gone", async () => {
    renderSettings();

    screen.getByRole("button", { name: "Sign out" }).click();

    await waitFor(() =>
      expect(navigate).toHaveBeenCalledWith("/auth/login", { replace: true }),
    );
  });

  // Signing out is a network round trip; two of them races the navigate.
  it("ignores a second press while the first is still going", async () => {
    renderSettings();

    const button = screen.getByRole("button", { name: "Sign out" });
    button.click();
    button.click();

    await waitFor(() => expect(logout).toHaveBeenCalledTimes(1));
  });
});
