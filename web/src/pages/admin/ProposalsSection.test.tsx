import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { DirectoryUser, Proposal } from "../../api/types";
import { NO_OPEN_PROPOSALS } from "../../lib/proposal";
import ProposalsSection from "./ProposalsSection";

/**
 * The town hall votes on things that happen the moment the vote lands, so what
 * is on screen has to be right about three things: what is being decided, what
 * passing does, and whether the person reading has a vote to cast.
 */

function proposal(overrides: Partial<Proposal> = {}): Proposal {
  return {
    id: "prop-1",
    type: "council_promotion",
    target_user_id: "user-1",
    target_display_name: "Ada Lovelace",
    rationale: "She has been moderating for a year.",
    created_by: "user-9",
    created_by_display_name: "Grace Hopper",
    status: "open",
    created_at: "2026-08-01T10:00:00Z",
    approve_count: 0,
    reject_count: 0,
    council_size: 5,
    my_vote: null,
    ...overrides,
  };
}

function directoryUser(overrides: Partial<DirectoryUser> = {}): DirectoryUser {
  return {
    id: "mod-1",
    display_name: "Mod One",
    role: "moderator",
    joined_at: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

/** Stubs the two requests the create dialog makes: the roll, then the proposal. */
function stubApi(roll: DirectoryUser[] = [directoryUser()]) {
  const created: string[] = [];

  const answer = (body: unknown, ok = true, status = 200) =>
    Promise.resolve({ ok, status, json: () => Promise.resolve(body) } as unknown as Response);

  vi.stubGlobal(
    "fetch",
    vi.fn((url: string, init?: RequestInit) => {
      if (url.includes("/admin/proposals")) {
        created.push(String(init?.body ?? ""));
        return answer(proposal({ id: "new-1" }), true, 201);
      }
      if (url.includes("/api/v1/users")) return answer({ users: roll, total: roll.length });
      return answer({});
    }),
  );

  return created;
}

function renderSection(
  proposals: Proposal[],
  options: { viewerId?: string; onVote?: (id: string, vote: "approve" | "reject") => void; onCreated?: () => void } = {},
) {
  const onVote = options.onVote ?? vi.fn();
  const onCreated = options.onCreated ?? vi.fn();

  render(
    <ProposalsSection
      proposals={proposals}
      viewerId={options.viewerId ?? "user-9"}
      onVote={onVote}
      voting={null}
      onCreated={onCreated}
    />,
  );

  return { onVote, onCreated };
}

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("Town Hall open proposals", () => {
  it("says what is being proposed in plain words, not in wire vocabulary", () => {
    renderSection([proposal()]);

    expect(screen.getByRole("heading", { name: "Promote Ada Lovelace to council" })).toBeInTheDocument();
    expect(screen.queryByText(/council_promotion/)).not.toBeInTheDocument();
  });

  it("shows who raised it and why", () => {
    renderSection([proposal()]);

    expect(screen.getByText(/Raised by Grace Hopper/)).toBeInTheDocument();
    expect(screen.getByText("She has been moderating for a year.")).toBeInTheDocument();
  });

  // The vote is carried out in the same request that decides it, so this has to
  // be readable before the buttons rather than discovered after them.
  it("warns that passing takes effect straight away", () => {
    renderSection([proposal()]);

    expect(screen.getByText(/straight away/)).toBeInTheDocument();
  });

  it("reports the tally as a sentence, out of that proposal's own electorate", () => {
    renderSection([proposal({ approve_count: 2, reject_count: 1, council_size: 5 })]);

    expect(screen.getByText("3 of 5 council members have voted — 2 in favour.")).toBeInTheDocument();
  });

  it("names each vote button after the proposal it belongs to", () => {
    renderSection([proposal()]);

    expect(
      screen.getByRole("button", { name: "Approve: Promote Ada Lovelace to council" }),
    ).toBeEnabled();
    expect(
      screen.getByRole("button", { name: "Decline: Promote Ada Lovelace to council" }),
    ).toBeEnabled();
  });

  it("casts the vote the button says it casts", () => {
    const { onVote } = renderSection([proposal()]);

    fireEvent.click(screen.getByRole("button", { name: /^Approve:/ }));

    expect(onVote).toHaveBeenCalledWith("prop-1", "approve");
  });

  // The server refuses a second vote, so the page must not offer one — and must
  // still say which way this member went.
  it("closes the buttons once this member has voted, and marks how they voted", () => {
    renderSection([proposal({ my_vote: "approve", approve_count: 1 })]);

    const approve = screen.getByRole("button", { name: /^Approve:/ });
    expect(approve).toBeDisabled();
    expect(approve).toHaveAttribute("aria-pressed", "true");
    expect(screen.getByRole("button", { name: /^Decline:/ })).toHaveAttribute(
      "aria-pressed",
      "false",
    );
    expect(screen.getByText("You voted in favour.")).toBeInTheDocument();
  });
});

describe("Town Hall self-removal", () => {
  const removal = proposal({
    id: "prop-removal",
    type: "council_removal",
    target_user_id: "user-9",
    target_display_name: "Grace Hopper",
    council_size: 4,
  });

  // Hiding a proposal about somebody from that somebody would be worse than
  // showing it; they simply are not part of its electorate.
  it("shows the target their own removal, with no vote to cast", () => {
    renderSection([removal], { viewerId: "user-9" });

    expect(
      screen.getByRole("heading", { name: "Remove Grace Hopper from council" }),
    ).toBeInTheDocument();
    expect(screen.getByText("You don't vote on your own removal.")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /^Approve:/ })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /^Decline:/ })).not.toBeInTheDocument();
  });

  it("still offers the vote to every other council member", () => {
    renderSection([removal], { viewerId: "user-3" });

    expect(screen.getByRole("button", { name: /^Approve:/ })).toBeEnabled();
    expect(screen.queryByText("You don't vote on your own removal.")).not.toBeInTheDocument();
  });

  // The target is out of the electorate, so the denominator differs from the
  // council's size — a tally out of the wrong one would name the wrong majority.
  it("counts the removal out of the electorate the server sent", () => {
    renderSection([{ ...removal, approve_count: 2, reject_count: 0 }], { viewerId: "user-9" });

    expect(screen.getByText("2 of 4 council members have voted — 2 in favour.")).toBeInTheDocument();
  });
});

describe("Town Hall history", () => {
  it("keeps a short record of what the council decided", () => {
    renderSection([
      proposal({ id: "p1", status: "passed", decided_at: "2026-08-02T09:00:00Z" }),
      proposal({
        id: "p2",
        type: "council_removal",
        status: "rejected",
        decided_at: "2026-07-30T09:00:00Z",
      }),
    ]);

    const history = screen.getByRole("heading", { name: "Already decided" }).parentElement;
    expect(within(history as HTMLElement).getByText(/^Passed /)).toBeInTheDocument();
    expect(within(history as HTMLElement).getByText(/^Rejected /)).toBeInTheDocument();
  });

  // A decided proposal is a record, not something still to be voted on.
  it("offers no vote buttons on a decided proposal", () => {
    renderSection([proposal({ status: "passed", decided_at: "2026-08-02T09:00:00Z" })]);

    expect(screen.queryByRole("button", { name: /^Approve:/ })).not.toBeInTheDocument();
  });

  it("says there is nothing open even when there is history below it", () => {
    renderSection([proposal({ status: "passed", decided_at: "2026-08-02T09:00:00Z" })]);

    expect(screen.getByText(NO_OPEN_PROPOSALS)).toBeInTheDocument();
  });
});

describe("Town Hall empty state", () => {
  it("invites the council to raise something rather than reporting a void", () => {
    renderSection([]);

    expect(screen.getByText(NO_OPEN_PROPOSALS)).toBeInTheDocument();
  });

  // An empty town hall is exactly when somebody reaches for this button.
  it("keeps the way to raise a proposal when there is nothing on the board", () => {
    renderSection([]);

    expect(screen.getByRole("button", { name: "Raise a proposal" })).toBeInTheDocument();
  });
});

describe("Raising a proposal", () => {
  function openDialog() {
    fireEvent.click(screen.getByRole("button", { name: "Raise a proposal" }));
    return screen.getByRole("dialog", { name: "Raise a proposal" });
  }

  it("asks what, who and why", async () => {
    stubApi();
    renderSection([]);
    openDialog();

    expect(screen.getByLabelText("What is being proposed?")).toBeInTheDocument();
    expect(await screen.findByLabelText("Who?")).toBeInTheDocument();
    expect(screen.getByLabelText("Why?")).toBeInTheDocument();
  });

  // Promoting offers moderators; removing offers the council. Offering the whole
  // roll would invite a proposal the server refuses.
  it("offers only moderators to promote", async () => {
    stubApi([
      directoryUser({ id: "mod-1", display_name: "Mod One", role: "moderator" }),
      directoryUser({ id: "c-1", display_name: "Council One", role: "council" }),
      directoryUser({ id: "m-1", display_name: "Ordinary Member", role: "member" }),
    ]);
    renderSection([]);
    openDialog();

    const picker = await screen.findByLabelText("Who?");
    expect(within(picker).getByRole("option", { name: "Mod One" })).toBeInTheDocument();
    expect(within(picker).queryByRole("option", { name: "Council One" })).not.toBeInTheDocument();
    expect(within(picker).queryByRole("option", { name: "Ordinary Member" })).not.toBeInTheDocument();
  });

  it("offers only council members to remove", async () => {
    stubApi([
      directoryUser({ id: "mod-1", display_name: "Mod One", role: "moderator" }),
      directoryUser({ id: "c-1", display_name: "Council One", role: "council" }),
    ]);
    renderSection([]);
    openDialog();
    await screen.findByLabelText("Who?");

    fireEvent.change(screen.getByLabelText("What is being proposed?"), {
      target: { value: "council_removal" },
    });

    const picker = screen.getByLabelText("Who?");
    expect(within(picker).getByRole("option", { name: "Council One" })).toBeInTheDocument();
    expect(within(picker).queryByRole("option", { name: "Mod One" })).not.toBeInTheDocument();
  });

  // Reopening approvals is about the town, so there is nobody to name.
  it("asks for nobody when the proposal is about the town", async () => {
    stubApi();
    renderSection([]);
    openDialog();
    await screen.findByLabelText("Who?");

    fireEvent.change(screen.getByLabelText("What is being proposed?"), {
      target: { value: "bootstrap_reentry" },
    });

    expect(screen.queryByLabelText("Who?")).not.toBeInTheDocument();
    expect(screen.getByText(/reaches 20 members again/)).toBeInTheDocument();
  });

  it("says so when there is nobody eligible, rather than showing an empty picker", async () => {
    stubApi([directoryUser({ role: "member" })]);
    renderSection([]);
    openDialog();

    expect(await screen.findByText("There are no moderators to promote yet.")).toBeInTheDocument();
  });

  it("will not send a proposal with no rationale", async () => {
    stubApi();
    renderSection([]);
    openDialog();
    await screen.findByLabelText("Who?");

    fireEvent.change(screen.getByLabelText("Who?"), { target: { value: "mod-1" } });

    expect(screen.getByRole("button", { name: "Put it to the council" })).toBeDisabled();
  });

  it("will not send a targeted proposal with nobody named", async () => {
    stubApi();
    renderSection([]);
    openDialog();
    await screen.findByLabelText("Who?");

    fireEvent.change(screen.getByLabelText("Why?"), { target: { value: "A year of good work." } });

    expect(screen.getByRole("button", { name: "Put it to the council" })).toBeDisabled();
  });

  it("sends what was chosen, and tells the page to reload", async () => {
    const created = stubApi();
    const { onCreated } = renderSection([]);
    openDialog();
    await screen.findByLabelText("Who?");

    fireEvent.change(screen.getByLabelText("Who?"), { target: { value: "mod-1" } });
    fireEvent.change(screen.getByLabelText("Why?"), {
      target: { value: "  A year of good work.  " },
    });
    fireEvent.click(screen.getByRole("button", { name: "Put it to the council" }));

    await waitFor(() =>
      expect(created).toEqual([
        JSON.stringify({
          type: "council_promotion",
          target_user_id: "mod-1",
          rationale: "A year of good work.",
        }),
      ]),
    );
    expect(onCreated).toHaveBeenCalled();
  });
});
