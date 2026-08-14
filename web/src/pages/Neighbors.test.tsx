import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { DirectoryUser } from "../api/types";
import { AuthProvider } from "../context/AuthContext";
import { NEIGHBORS_PAGE_SIZE } from "../hooks/useNeighbors";
import { NEIGHBORS_INVITE } from "../lib/directory";
import Neighbors from "./Neighbors";

/**
 * The directory is the one page a pending member can act on: every name here is
 * somebody who could vouch for them. These pin that they are told so, that a
 * member with no display name is still findable, and that searching asks the
 * server rather than filtering the page it happens to have.
 */

function neighbor(overrides: Partial<DirectoryUser> = {}): DirectoryUser {
  return {
    id: "0193a7b2-aaaa-7000-8000-000000000000",
    display_name: "Ada Lovelace",
    role: "member",
    joined_at: "2026-03-01T12:00:00Z",
    ...overrides,
  };
}

interface StubOptions {
  users?: DirectoryUser[];
  total?: number;
  /** The signed-in viewer's role, which decides whether the invite is shown. */
  viewerRole?: string;
  /** Answers the directory read with a 500 instead. */
  failDirectory?: boolean;
}

/**
 * Stubs the three requests a rendered page makes: the Kratos session, the
 * viewer's own profile, and the directory itself. Directory requests are
 * recorded so a test can assert what the search actually asked for — filtering
 * on the client would look identical on screen and be wrong the moment the list
 * is longer than a page.
 */
function stubApi(options: StubOptions = {}) {
  const { users = [neighbor()], total = users.length, viewerRole = "member" } = options;
  const directoryCalls: string[] = [];

  const answer = (body: unknown, ok = true, status = 200) =>
    Promise.resolve({ ok, status, json: () => Promise.resolve(body) } as unknown as Response);

  vi.stubGlobal(
    "fetch",
    vi.fn((url: string) => {
      if (url.startsWith("/.ory")) return answer({ id: "session-1", active: true });
      if (url.startsWith("/api/v1/me")) {
        return answer({
          id: "viewer-1",
          display_name: "Viewer",
          bio: "",
          avatar_url: "",
          trust_score: 50,
          role: viewerRole,
          is_active: true,
          joined_at: "2026-01-01T00:00:00Z",
        });
      }
      if (url.startsWith("/api/v1/users")) {
        directoryCalls.push(url);
        if (options.failDirectory) return answer({ error: "internal error" }, false, 500);
        const offset = Number(new URL(url, "http://test").searchParams.get("offset") ?? 0);
        return answer({ users: users.slice(offset, offset + NEIGHBORS_PAGE_SIZE), total });
      }
      return answer({});
    }),
  );

  return directoryCalls;
}

function renderPage() {
  return render(
    <MemoryRouter>
      <AuthProvider>
        <Neighbors />
      </AuthProvider>
    </MemoryRouter>,
  );
}

/** The most recent directory request, which is the one the list is showing. */
const lastCall = (calls: string[]) => calls[calls.length - 1];

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("Neighbors listing", () => {
  it("lists each neighbour as a link to their profile", async () => {
    stubApi();

    renderPage();

    const link = await screen.findByRole("link", { name: /Ada Lovelace/ });
    expect(link).toHaveAttribute("href", "/profile/0193a7b2-aaaa-7000-8000-000000000000");
  });

  it("shows when somebody joined, to the month", async () => {
    stubApi();

    renderPage();

    expect(await screen.findByText(/Joined March 2026/)).toBeInTheDocument();
  });

  // "Pending" is the vocabulary of a processing queue; these are the people the
  // page exists to have welcomed.
  it("greets a newcomer as new here rather than as pending", async () => {
    stubApi({ users: [neighbor({ role: "pending" })] });

    renderPage();

    expect(await screen.findByText(/New here/)).toBeInTheDocument();
    expect(screen.queryByText(/pending/i)).not.toBeInTheDocument();
  });

  // A member with no display name is exactly the member most in need of being
  // found, so the row must still identify them.
  it("falls back to the id prefix for a neighbour with no display name", async () => {
    stubApi({ users: [neighbor({ display_name: "" })] });

    renderPage();

    expect(await screen.findByRole("link", { name: /0193a7b2\.\.\./ })).toBeInTheDocument();
  });

  it("says how many of the roll are on screen", async () => {
    stubApi({ users: Array.from({ length: NEIGHBORS_PAGE_SIZE }, (_, i) =>
      neighbor({ id: `user-${i}`, display_name: `Member ${i}` }),
    ), total: 112 });

    renderPage();

    expect(
      await screen.findByText(`Showing ${NEIGHBORS_PAGE_SIZE} of 112 neighbours`),
    ).toBeInTheDocument();
  });

  it("offers more, and appends the next page rather than replacing it", async () => {
    const users = Array.from({ length: NEIGHBORS_PAGE_SIZE + 3 }, (_, i) =>
      neighbor({ id: `user-${i}`, display_name: `Member ${i}` }),
    );
    stubApi({ users });

    renderPage();
    const more = await screen.findByRole("button", { name: /show more/i });
    fireEvent.click(more);

    await waitFor(() =>
      expect(screen.getAllByRole("listitem")).toHaveLength(NEIGHBORS_PAGE_SIZE + 3),
    );
    expect(screen.getByText("Member 0")).toBeInTheDocument();
  });

  // Everyone is loaded, so the button would fetch a page that comes back empty.
  it("stops offering more once the whole roll is loaded", async () => {
    stubApi({ users: [neighbor()], total: 1 });

    renderPage();

    await screen.findByRole("link", { name: /Ada Lovelace/ });
    expect(screen.queryByRole("button", { name: /show more/i })).not.toBeInTheDocument();
  });

  it("says so, and offers a retry, when the directory cannot be read", async () => {
    stubApi({ failDirectory: true });

    renderPage();

    expect(await screen.findByText(/could not load the directory/i)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Retry" })).toBeInTheDocument();
  });
});

describe("Neighbors search", () => {
  it("has a labelled search box", async () => {
    stubApi();
    renderPage();

    expect(await screen.findByLabelText("Find a neighbour")).toBeInTheDocument();
  });

  // Filtering on the client would look identical on the first page and be wrong
  // for every name past it.
  it("asks the server for the search, rather than filtering what it has", async () => {
    const calls = stubApi();
    renderPage();
    await screen.findByRole("link", { name: /Ada Lovelace/ });

    fireEvent.change(screen.getByLabelText("Find a neighbour"), { target: { value: "ali" } });

    await waitFor(() => expect(lastCall(calls)).toContain("q=ali"));
  });

  it("does not send a q at all for an empty box", async () => {
    const calls = stubApi();
    renderPage();

    await waitFor(() => expect(calls).toHaveLength(1));
    expect(lastCall(calls)).not.toContain("q=");
  });

  it("encodes a search that contains characters a url cares about", async () => {
    const calls = stubApi();
    renderPage();
    await screen.findByRole("link", { name: /Ada Lovelace/ });

    fireEvent.change(screen.getByLabelText("Find a neighbour"), {
      target: { value: "ada & grace" },
    });

    await waitFor(() => expect(lastCall(calls)).toContain("q=ada+%26+grace"));
  });

  it("says nobody matched rather than showing an empty page", async () => {
    const calls = stubApi({ users: [] });
    renderPage();
    await waitFor(() => expect(calls).toHaveLength(1));

    fireEvent.change(screen.getByLabelText("Find a neighbour"), { target: { value: "zzz" } });

    expect(await screen.findByText("Nobody by that name yet.")).toBeInTheDocument();
  });
});

describe("Neighbors invitation", () => {
  it("tells a pending member that any of these people can ring them in", async () => {
    stubApi({ viewerRole: "pending" });

    renderPage();

    const invite = await screen.findByTestId("neighbors-invite");
    expect(within(invite).getByRole("heading", { name: NEIGHBORS_INVITE.title })).toBeInTheDocument();
    expect(invite).toHaveTextContent(NEIGHBORS_INVITE.body);
  });

  // A member who is already in has nothing to be invited to.
  it.each(["member", "moderator", "council"])("shows no invitation to a %s", async (role) => {
    stubApi({ viewerRole: role });

    renderPage();

    await screen.findByRole("link", { name: /Ada Lovelace/ });
    expect(screen.queryByTestId("neighbors-invite")).not.toBeInTheDocument();
  });
});
