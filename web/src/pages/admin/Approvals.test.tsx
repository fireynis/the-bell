import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { User } from "../../api/types";
import { APPROVALS_EMPTY, APPROVALS_NO_MATCH, APPROVALS_PAGE_SIZE } from "../../lib/approvals";
import { NO_RESIDENCY_CLAIM } from "../../lib/residency";
import Approvals from "./Approvals";

/**
 * The council's approval queue. It is the one screen where somebody decides
 * about a stranger, so what it has to be right about is: who is waiting and how
 * long, what they said about themselves and how that reads, that searching asks
 * the server rather than filtering the page in hand, and that an approval both
 * lands and leaves the list.
 */

function applicant(overrides: Partial<User> = {}): User {
  return {
    id: "0193a7b2-aaaa-7000-8000-000000000000",
    display_name: "Ada Lovelace",
    bio: "",
    avatar_url: "",
    trust_score: 50,
    role: "pending",
    is_active: true,
    joined_at: "2026-03-01T12:00:00Z",
    ...overrides,
  };
}

interface StubOptions {
  users?: User[];
  total?: number;
  /** Answers the queue read with this status instead of a page. */
  failQueueWith?: number;
  /** Answers the approve write with a 400 and this message. */
  approveError?: string;
}

/**
 * Stubs the queue read and the approve write, recording the URLs the queue was
 * asked for. The requests are recorded because filtering on the client would
 * look identical on the first page and be wrong for every applicant past it.
 */
function stubApi(options: StubOptions = {}) {
  const { users = [applicant()], total = users.length } = options;
  const queueCalls: string[] = [];
  const approved: string[] = [];

  const answer = (body: unknown, ok = true, status = 200) =>
    Promise.resolve({ ok, status, json: () => Promise.resolve(body) } as unknown as Response);

  vi.stubGlobal(
    "fetch",
    vi.fn((url: string) => {
      if (url.includes("/vouches/approve/")) {
        approved.push(url.split("/vouches/approve/")[1]);
        if (options.approveError) {
          return answer({ error: options.approveError }, false, 400);
        }
        return answer(applicant({ role: "member" }));
      }
      if (url.includes("/vouches/pending")) {
        queueCalls.push(url);
        if (options.failQueueWith) {
          return answer({ error: "refused" }, false, options.failQueueWith);
        }
        const offset = Number(new URL(url, "http://test").searchParams.get("offset") ?? 0);
        return answer({ users: users.slice(offset, offset + APPROVALS_PAGE_SIZE), total });
      }
      return answer({});
    }),
  );

  return { queueCalls, approved };
}

function renderPage() {
  return render(
    <MemoryRouter>
      <Approvals />
    </MemoryRouter>,
  );
}

/** The most recent queue request, which is the one the list is showing. */
const lastCall = (calls: string[]) => calls[calls.length - 1];

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("Approvals queue", () => {
  it("names each applicant as a link to their profile", async () => {
    stubApi();

    renderPage();

    expect(await screen.findByRole("link", { name: "Ada Lovelace" })).toHaveAttribute(
      "href",
      "/profile/0193a7b2-aaaa-7000-8000-000000000000",
    );
  });

  it("says the day somebody joined, not just the month", async () => {
    stubApi();

    renderPage();

    expect(await screen.findByText(/Joined March 1, 2026/)).toBeInTheDocument();
  });

  // The council is being asked to act on the wait, so the wait is what the row
  // says — the join date alone leaves the arithmetic to the reader. Measured
  // from the real clock rather than a frozen one: fake timers and the async
  // queries used throughout this file do not coexist.
  it("says how long somebody has been waiting", async () => {
    const twelveDaysAgo = new Date(Date.now() - 12 * 24 * 60 * 60 * 1000).toISOString();
    stubApi({ users: [applicant({ joined_at: twelveDaysAgo })] });

    renderPage();

    expect(await screen.findByText(/Waiting 12 days/)).toBeInTheDocument();
  });

  // A member with no display name is exactly the member most in need of being
  // identifiable, since a council member still has to be able to approve them.
  it("falls back to the id prefix for an applicant with no display name", async () => {
    stubApi({ users: [applicant({ display_name: "" })] });

    renderPage();

    expect(await screen.findByRole("link", { name: /0193a7b2\.\.\./ })).toBeInTheDocument();
  });

  it("asks for the longest-waiting page rather than the whole queue", async () => {
    const { queueCalls } = stubApi();

    renderPage();

    await waitFor(() => expect(queueCalls).toHaveLength(1));
    expect(lastCall(queueCalls)).toContain(`limit=${APPROVALS_PAGE_SIZE}`);
    expect(lastCall(queueCalls)).toContain("offset=0");
  });

  it("says how many of the queue are on screen", async () => {
    stubApi({
      users: Array.from({ length: APPROVALS_PAGE_SIZE }, (_, i) =>
        applicant({ id: `user-${i}`, display_name: `Waiting ${i}` }),
      ),
      total: 112,
    });

    renderPage();

    expect(
      await screen.findByText(`Showing ${APPROVALS_PAGE_SIZE} of 112 neighbours waiting`),
    ).toBeInTheDocument();
  });

  it("offers more, and appends the next page rather than replacing it", async () => {
    const users = Array.from({ length: APPROVALS_PAGE_SIZE + 3 }, (_, i) =>
      applicant({ id: `user-${i}`, display_name: `Waiting ${i}` }),
    );
    stubApi({ users });

    renderPage();
    fireEvent.click(await screen.findByRole("button", { name: /show more/i }));

    await waitFor(() =>
      expect(screen.getAllByRole("listitem")).toHaveLength(APPROVALS_PAGE_SIZE + 3),
    );
    expect(screen.getByText("Waiting 0")).toBeInTheDocument();
  });

  // Everyone is loaded, so the button would fetch a page that comes back empty.
  it("stops offering more once the whole queue is loaded", async () => {
    stubApi({ users: [applicant()], total: 1 });

    renderPage();

    await screen.findByRole("link", { name: "Ada Lovelace" });
    expect(screen.queryByRole("button", { name: /show more/i })).not.toBeInTheDocument();
  });

  it("says the town is caught up rather than showing an empty list", async () => {
    stubApi({ users: [], total: 0 });

    renderPage();

    expect(await screen.findByText(APPROVALS_EMPTY)).toBeInTheDocument();
  });

  it("says so, and offers a retry, when the queue cannot be read", async () => {
    stubApi({ failQueueWith: 500 });

    renderPage();

    expect(await screen.findByText(/could not load the approval queue/i)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Retry" })).toBeInTheDocument();
  });

  // A 403 is the town having left bootstrap mode, not an outage. Reporting it
  // as one sends a council member hunting for a problem that does not exist.
  it("explains a closed queue rather than reporting a failure", async () => {
    stubApi({ failQueueWith: 403 });

    renderPage();

    expect(await screen.findByText(/left bootstrap mode/i)).toBeInTheDocument();
    expect(screen.queryByText(/could not load the approval queue/i)).not.toBeInTheDocument();
  });
});

describe("Approvals residency claims", () => {
  it("shows the claim as something the newcomer says, not as their address", async () => {
    stubApi({ users: [applicant({ residency_claim: "the old mill road" })] });

    renderPage();

    expect(
      await screen.findByText("Says they're at or near the old mill road"),
    ).toBeInTheDocument();
  });

  it("says quietly that no address was given rather than leaving a gap", async () => {
    stubApi({ users: [applicant({ residency_claim: "" })] });

    renderPage();

    expect(await screen.findByText(NO_RESIDENCY_CLAIM)).toBeInTheDocument();
  });

  // The field is absent entirely on a build whose server does not send it,
  // which must read the same as somebody who chose not to answer.
  it("says the same when the row carries no claim at all", async () => {
    stubApi();

    renderPage();

    expect(await screen.findByText(NO_RESIDENCY_CLAIM)).toBeInTheDocument();
  });
});

describe("Approvals search", () => {
  it("has a labelled search box", async () => {
    stubApi();
    renderPage();

    expect(await screen.findByLabelText("Find someone waiting")).toBeInTheDocument();
  });

  it("asks the server for the search, rather than filtering what it has", async () => {
    const { queueCalls } = stubApi();
    renderPage();
    await screen.findByRole("link", { name: "Ada Lovelace" });

    fireEvent.change(screen.getByLabelText("Find someone waiting"), {
      target: { value: "ali" },
    });

    await waitFor(() => expect(lastCall(queueCalls)).toContain("q=ali"));
  });

  it("does not send a q at all for an empty box", async () => {
    const { queueCalls } = stubApi();
    renderPage();

    await waitFor(() => expect(queueCalls).toHaveLength(1));
    expect(lastCall(queueCalls)).not.toContain("q=");
  });

  it("encodes a search that contains characters a url cares about", async () => {
    const { queueCalls } = stubApi();
    renderPage();
    await screen.findByRole("link", { name: "Ada Lovelace" });

    fireEvent.change(screen.getByLabelText("Find someone waiting"), {
      target: { value: "ada & grace" },
    });

    await waitFor(() => expect(lastCall(queueCalls)).toContain("q=ada+%26+grace"));
  });

  // Different from an empty queue: one is good news, the other is a search
  // that found nothing.
  it("says nobody matched rather than that the town is caught up", async () => {
    const { queueCalls } = stubApi({ users: [], total: 0 });
    renderPage();
    await waitFor(() => expect(queueCalls).toHaveLength(1));

    fireEvent.change(screen.getByLabelText("Find someone waiting"), {
      target: { value: "zzz" },
    });

    expect(await screen.findByText(APPROVALS_NO_MATCH)).toBeInTheDocument();
  });
});

describe("Approvals approving", () => {
  // Named for the person, not just the action: twenty-five buttons all called
  // "Approve" are indistinguishable to somebody listening rather than looking.
  it("names the person on the approve button", async () => {
    stubApi();

    renderPage();

    expect(await screen.findByRole("button", { name: "Approve Ada Lovelace" })).toBeEnabled();
  });

  it("rings the applicant in and takes them out of the queue", async () => {
    const { approved } = stubApi({
      users: [
        applicant({ id: "user-1", display_name: "Ada Lovelace" }),
        applicant({ id: "user-2", display_name: "Grace Hopper" }),
      ],
      total: 2,
    });

    renderPage();
    fireEvent.click(await screen.findByRole("button", { name: "Approve Ada Lovelace" }));

    await waitFor(() =>
      expect(screen.queryByRole("link", { name: "Ada Lovelace" })).not.toBeInTheDocument(),
    );
    expect(approved).toEqual(["user-1"]);
    expect(screen.getByRole("link", { name: "Grace Hopper" })).toBeInTheDocument();
  });

  // The count is what the page says about the queue, so an approval that left
  // it alone would leave the heading contradicting the list underneath it.
  it("takes the approved neighbour off the count as well as the list", async () => {
    stubApi({
      users: [
        applicant({ id: "user-1", display_name: "Ada Lovelace" }),
        applicant({ id: "user-2", display_name: "Grace Hopper" }),
      ],
      total: 2,
    });

    renderPage();
    await screen.findByText("2 neighbours waiting");

    fireEvent.click(screen.getByRole("button", { name: "Approve Ada Lovelace" }));

    expect(await screen.findByText("1 neighbour waiting")).toBeInTheDocument();
  });

  it("says what went wrong and keeps the applicant in the queue when the approval fails", async () => {
    stubApi({ approveError: "user is not pending" });

    renderPage();
    fireEvent.click(await screen.findByRole("button", { name: "Approve Ada Lovelace" }));

    expect(await screen.findByText("user is not pending")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Ada Lovelace" })).toBeInTheDocument();
  });
});
