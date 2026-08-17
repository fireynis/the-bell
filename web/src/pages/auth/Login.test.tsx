import { render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { RegistrationMode } from "../../api/types";
import { AuthProvider } from "../../context/AuthContext";
import { ThemeProvider } from "../../context/ThemeContext";
import { LOGIN_INVITE_LINK, LOGIN_INVITE_ONLY } from "../../lib/invite";
import Login from "./Login";

/**
 * The way in, for somebody who does not have an account yet.
 *
 * "Register" promises a form, and in a town that admits people by invitation
 * there is no form behind that link — sending somebody to a page that explains
 * they cannot register is a worse experience than telling them here.
 */

const kratosFlow = {
  id: "flow-1",
  type: "browser",
  ui: {
    action: "/.ory/self-service/login",
    method: "POST",
    nodes: [
      {
        type: "input",
        group: "default",
        attributes: { node_type: "input", name: "identifier", type: "text", required: true },
        meta: { label: { id: 1, text: "E-Mail", type: "info" } },
      },
    ],
  },
};

function stubApi(mode: RegistrationMode) {
  const answer = (body: unknown) =>
    Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve(body) } as unknown as Response);

  vi.stubGlobal(
    "fetch",
    vi.fn((url: string) => {
      if (url.startsWith("/api/v1/config")) {
        return answer({ town_name: "Millbrook", registration_mode: mode });
      }
      if (url.includes("self-service/login")) return answer(kratosFlow);
      return answer(null);
    }),
  );
}

function renderPage() {
  return render(
    <MemoryRouter>
      <ThemeProvider>
        <AuthProvider>
          <Login />
        </AuthProvider>
      </ThemeProvider>
    </MemoryRouter>,
  );
}

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("Login's way in for a newcomer", () => {
  it("says the town is invitation-only, and links to the explanation", async () => {
    stubApi("invite");

    renderPage();

    // Substring rather than exact: the sentence shares its element with the link.
    expect(await screen.findByText(LOGIN_INVITE_ONLY, { exact: false })).toBeInTheDocument();
    const link = screen.getByRole("link", { name: LOGIN_INVITE_LINK });
    expect(link).toHaveAttribute("href", "/auth/registration");
    expect(screen.queryByRole("link", { name: /Register/ })).not.toBeInTheDocument();
  });

  it("offers registration as it always did in an open town", async () => {
    stubApi("open");

    renderPage();

    const link = await screen.findByRole("link", { name: "Don't have an account? Register" });
    expect(link).toHaveAttribute("href", "/auth/registration");
    expect(screen.queryByText(LOGIN_INVITE_ONLY, { exact: false })).not.toBeInTheDocument();
  });

  it("still offers password recovery either way", async () => {
    stubApi("invite");

    renderPage();

    await waitFor(() =>
      expect(screen.getByRole("link", { name: "Forgot your password?" })).toHaveAttribute(
        "href",
        "/auth/recovery",
      ),
    );
  });
});
