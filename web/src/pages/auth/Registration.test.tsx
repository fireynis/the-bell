import { render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, useLocation } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { RegistrationMode } from "../../api/types";
import { AuthProvider } from "../../context/AuthContext";
import { ThemeProvider } from "../../context/ThemeContext";
import {
  INVITE_COOKIE_NAME,
  INVITE_EXPIRED_NOTICE,
  INVITE_ONLY_NOTICE,
} from "../../lib/invite";
import Registration from "./Registration";

/**
 * Who is allowed to see a registration form.
 *
 * In invite mode the server refuses an uninvited registration at the Kratos
 * proxy, and it reads the token out of a cookie this page sets. Two things here
 * are therefore correctness rather than presentation: no form is rendered when
 * there is nothing behind it to submit to, and the cookie is written before the
 * flow is created — a cookie set afterwards is a cookie set too late, and the
 * failure looks like a registration page that cannot load.
 */

/** A Kratos registration flow with the two fields the form actually renders. */
const kratosFlow = {
  id: "flow-1",
  type: "browser",
  ui: {
    action: "/.ory/self-service/registration",
    method: "POST",
    nodes: [
      {
        type: "input",
        group: "default",
        attributes: {
          node_type: "input",
          name: "traits.email",
          type: "email",
          required: true,
        },
        meta: { label: { id: 1, text: "E-Mail", type: "info" } },
      },
      {
        type: "input",
        group: "password",
        attributes: { node_type: "input", name: "password", type: "password", required: true },
        meta: { label: { id: 2, text: "Password", type: "info" } },
      },
      {
        type: "input",
        group: "password",
        attributes: { node_type: "input", name: "method", type: "submit", value: "password" },
        meta: { label: { id: 3, text: "Sign up", type: "info" } },
      },
    ],
  },
};

interface StubOptions {
  mode?: RegistrationMode;
  /** What the public lookup answers; a number is the status it fails with. */
  lookup?: { email: string; town_name: string; inviter_display_name: string } | number;
}

/**
 * Stubs the config read, the invite lookup and the Kratos flow.
 *
 * Every request is recorded in order, and the cookie is snapshotted at the
 * moment the flow is created — which is the only way to see whether the token
 * was in place when the proxy would have looked for it.
 */
function stubApi(options: StubOptions = {}) {
  const { mode = "invite" } = options;
  const calls: string[] = [];
  /** document.cookie as it stood when the registration flow was requested. */
  let cookieAtFlow: string | null = null;

  const answer = (body: unknown, ok = true, status = 200) =>
    Promise.resolve({ ok, status, json: () => Promise.resolve(body) } as unknown as Response);

  vi.stubGlobal(
    "fetch",
    vi.fn((url: string) => {
      calls.push(url);

      if (url.startsWith("/api/v1/config")) {
        return answer({ town_name: "Millbrook", registration_mode: mode });
      }
      if (url.startsWith("/api/v1/invites/lookup")) {
        if (typeof options.lookup === "number") {
          return answer({ error: "not found" }, false, options.lookup);
        }
        return answer({ ...options.lookup, status: "open" });
      }
      if (url.includes("self-service/registration")) {
        cookieAtFlow = document.cookie;
        return answer(kratosFlow);
      }
      if (url.startsWith("/.ory")) return answer(null);
      return answer({});
    }),
  );

  return { calls, cookieAtFlow: () => cookieAtFlow };
}

/** Reports the live query string, so a test can watch what useFlow does to it. */
function LocationProbe() {
  return <span data-testid="location">{useLocation().search}</span>;
}

function renderPage(path = "/auth/registration") {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <ThemeProvider>
        <AuthProvider>
          <Registration />
          <LocationProbe />
        </AuthProvider>
      </ThemeProvider>
    </MemoryRouter>,
  );
}

const emailField = () => screen.getByLabelText("E-Mail");

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  document.cookie = `${INVITE_COOKIE_NAME}=; Path=/; Max-Age=0`;
});

describe("Registration with a live invitation", () => {
  const lookup = {
    email: "newcomer@example.com",
    town_name: "Millbrook",
    inviter_display_name: "Ada Lovelace",
  };

  it("greets the newcomer by the name of whoever invited them", async () => {
    stubApi({ lookup });

    renderPage("/auth/registration?invite=tok-abc");

    expect(
      await screen.findByText("Ada Lovelace invited you to join Millbrook — welcome."),
    ).toBeInTheDocument();
  });

  // The server refuses a registration whose address does not match. The lock is
  // how that rule is communicated rather than discovered on submission.
  it("fills the email in from the invitation and will not let it be changed", async () => {
    stubApi({ lookup });

    renderPage("/auth/registration?invite=tok-abc");

    await waitFor(() => expect(emailField()).toHaveValue("newcomer@example.com"));
    expect(emailField()).toHaveAttribute("readonly");
    expect(screen.getByText(/Your invitation is for newcomer@example\.com/)).toBeInTheDocument();
  });

  // Read-only rather than disabled: a disabled input is left out of FormData,
  // so locking it that way would submit a registration with no address at all.
  it("keeps the locked address submittable", async () => {
    stubApi({ lookup });

    renderPage("/auth/registration?invite=tok-abc");

    await waitFor(() => expect(emailField()).toHaveValue("newcomer@example.com"));
    expect(emailField()).not.toBeDisabled();
  });

  it("writes the token where the proxy reads it, before the flow is created", async () => {
    const { cookieAtFlow } = stubApi({ lookup });

    renderPage("/auth/registration?invite=tok-abc");

    await waitFor(() => expect(cookieAtFlow()).not.toBeNull());
    expect(cookieAtFlow()).toContain(`${INVITE_COOKIE_NAME}=tok-abc`);
    expect(document.cookie).toContain(`${INVITE_COOKIE_NAME}=tok-abc`);
  });

  // useFlow stamps the flow id into the URL, and doing that with a bare object
  // replaces the whole query string. Losing the token there flips the page from
  // the greeting to the invitation-only explanation while the reader is looking
  // at it, and the form they were about to fill in disappears.
  it("keeps the invitation in the URL when the flow id is stamped into it", async () => {
    stubApi({ lookup });

    renderPage("/auth/registration?invite=tok-abc");

    await waitFor(() =>
      expect(screen.getByTestId("location")).toHaveTextContent("flow=flow-1"),
    );
    expect(screen.getByTestId("location")).toHaveTextContent("invite=tok-abc");
    expect(screen.getByTestId("invited-greeting")).toBeInTheDocument();
    expect(screen.queryByText(INVITE_ONLY_NOTICE.title)).not.toBeInTheDocument();
  });

  it("does not ask Kratos for a flow until the invitation has checked out", async () => {
    const { calls } = stubApi({ lookup });

    renderPage("/auth/registration?invite=tok-abc");

    await waitFor(() => expect(calls.some((c) => c.includes("self-service/registration"))).toBe(true));
    const lookupAt = calls.findIndex((c) => c.startsWith("/api/v1/invites/lookup"));
    const flowAt = calls.findIndex((c) => c.includes("self-service/registration"));
    expect(lookupAt).toBeGreaterThanOrEqual(0);
    expect(lookupAt).toBeLessThan(flowAt);
  });
});

describe("Registration with a spent invitation", () => {
  it("says the link is no longer good, gently, and explains the way in", async () => {
    stubApi({ lookup: 404 });

    renderPage("/auth/registration?invite=tok-stale");

    expect(await screen.findByText(INVITE_EXPIRED_NOTICE)).toBeInTheDocument();
    expect(screen.getByText(INVITE_ONLY_NOTICE.title)).toBeInTheDocument();
  });

  it("offers no form to fill in", async () => {
    stubApi({ lookup: 404 });

    renderPage("/auth/registration?invite=tok-stale");

    await screen.findByText(INVITE_EXPIRED_NOTICE);
    expect(screen.queryByLabelText("E-Mail")).not.toBeInTheDocument();
  });

  // A 404 is the answer for used, withdrawn, lapsed and invented alike, so the
  // cookie would only make the proxy refuse a flow this page is not offering.
  it("sets no cookie for a token the server would not vouch for", async () => {
    stubApi({ lookup: 404 });

    renderPage("/auth/registration?invite=tok-stale");

    await screen.findByText(INVITE_EXPIRED_NOTICE);
    expect(document.cookie).not.toContain(INVITE_COOKIE_NAME);
  });

  // In an open town the invitation was never what let them in.
  it("still offers the form in an open town, saying what the link was", async () => {
    stubApi({ mode: "open", lookup: 404 });

    renderPage("/auth/registration?invite=tok-stale");

    expect(await screen.findByText(INVITE_EXPIRED_NOTICE)).toBeInTheDocument();
    await waitFor(() => expect(screen.getByLabelText("E-Mail")).toBeInTheDocument());
    expect(emailField()).not.toHaveAttribute("readonly");
  });
});

describe("Registration with no invitation", () => {
  it("explains an invitation-only town instead of showing a form", async () => {
    stubApi();

    renderPage();

    expect(await screen.findByText(INVITE_ONLY_NOTICE.title)).toBeInTheDocument();
    expect(screen.getByText(INVITE_ONLY_NOTICE.body("Millbrook"))).toBeInTheDocument();
    expect(screen.getByText(INVITE_ONLY_NOTICE.ask("Millbrook"))).toBeInTheDocument();
    expect(screen.queryByLabelText("E-Mail")).not.toBeInTheDocument();
  });

  it("points somebody who already has an account at signing in", async () => {
    stubApi();

    renderPage();

    const link = await screen.findByRole("link", { name: INVITE_ONLY_NOTICE.signIn });
    expect(link).toHaveAttribute("href", "/auth/login");
  });

  // No Kratos flow is created at all: there is nothing to submit it with.
  it("asks Kratos for nothing", async () => {
    const { calls } = stubApi();

    renderPage();

    await screen.findByText(INVITE_ONLY_NOTICE.title);
    expect(calls.some((c) => c.includes("self-service/registration"))).toBe(false);
  });

  it("leaves an open town's registration exactly as it was", async () => {
    stubApi({ mode: "open" });

    renderPage();

    await waitFor(() => expect(screen.getByLabelText("E-Mail")).toBeInTheDocument());
    expect(emailField()).not.toHaveAttribute("readonly");
    expect(screen.queryByText(INVITE_ONLY_NOTICE.title)).not.toBeInTheDocument();
  });
});
