import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { CreateInviteResponse } from "../../api/types";
import { INVITE_DIALOG } from "../../lib/invite";
import InviteDialog from "./InviteDialog";

/**
 * Sending an invitation.
 *
 * Two things here are load-bearing. The sender is told, before they send, that
 * this is a vouch and what it costs — an invitation that arrives as a surprise
 * obligation is how somebody ends up staking their standing on a stranger. And
 * the link comes back on screen: the raw token exists in that one response and
 * is never sent again, so a dialog that closed on success would destroy the only
 * copy every time the town's email is misconfigured.
 */

function created(overrides: Partial<CreateInviteResponse> = {}): CreateInviteResponse {
  return {
    invite: {
      id: "i1",
      email: "newcomer@example.com",
      note: "",
      status: "open",
      created_at: "2026-03-01T00:00:00Z",
      expires_at: "2026-03-15T00:00:00Z",
    },
    invite_url: "https://bell.example.test/auth/registration?invite=tok-abc",
    email_sent: true,
    ...overrides,
  };
}

interface StubOptions {
  response?: CreateInviteResponse;
  /** Answers the create with this status and error body instead. */
  fail?: { status: number; error: string };
}

/** Stubs POST /api/v1/invites and records what was sent. */
function stubCreate(options: StubOptions = {}) {
  const sent: Array<Record<string, unknown>> = [];

  vi.stubGlobal(
    "fetch",
    vi.fn((_url: string, init?: RequestInit) => {
      if (init?.body) sent.push(JSON.parse(init.body as string));
      if (options.fail) {
        return Promise.resolve({
          ok: false,
          status: options.fail.status,
          json: () => Promise.resolve({ error: options.fail!.error }),
        } as unknown as Response);
      }
      return Promise.resolve({
        ok: true,
        status: 201,
        json: () => Promise.resolve(options.response ?? created()),
      } as unknown as Response);
    }),
  );

  return sent;
}

const emailField = () => screen.getByLabelText(INVITE_DIALOG.emailLabel);
const noteField = () => screen.getByLabelText(INVITE_DIALOG.noteLabel);
const sendButton = () => screen.getByRole("button", { name: INVITE_DIALOG.submit });

function renderDialog(onInvited = () => {}, onClose = () => {}) {
  return render(<InviteDialog onClose={onClose} onInvited={onInvited} />);
}

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("InviteDialog before sending", () => {
  it("says the invitation is a vouch, and what it spends", () => {
    stubCreate();
    renderDialog();

    expect(screen.getByText(/Inviting someone is vouching for them/)).toBeInTheDocument();
    expect(screen.getByText(/today's three vouches/)).toBeInTheDocument();
  });

  it("cannot be sent with an empty address", () => {
    stubCreate();
    renderDialog();

    expect(sendButton()).toBeDisabled();
  });

  it("cannot be sent with something that is not an address", () => {
    stubCreate();
    renderDialog();

    fireEvent.change(emailField(), { target: { value: "nobody" } });

    expect(sendButton()).toBeDisabled();
  });

  // The counter is the server's rule rather than a target, so it stays out of
  // the way until the limit is in sight.
  it("counts the note down only once the limit is in sight", () => {
    stubCreate();
    renderDialog();

    fireEvent.change(noteField(), { target: { value: "We met at the market." } });
    expect(screen.queryByText(/characters left/)).not.toBeInTheDocument();

    fireEvent.change(noteField(), { target: { value: "a".repeat(480) } });
    expect(screen.getByText("20 characters left")).toBeInTheDocument();
  });

  it("refuses to send a note past the limit", () => {
    stubCreate();
    renderDialog();

    fireEvent.change(emailField(), { target: { value: "newcomer@example.com" } });
    fireEvent.change(noteField(), { target: { value: "a".repeat(501) } });

    expect(sendButton()).toBeDisabled();
  });

  it("sends the address and the note, both trimmed", async () => {
    const sent = stubCreate();
    renderDialog();

    fireEvent.change(emailField(), { target: { value: "  newcomer@example.com " } });
    fireEvent.change(noteField(), { target: { value: "  We met at the market.  " } });
    fireEvent.click(sendButton());

    await waitFor(() => expect(sent).toHaveLength(1));
    expect(sent[0]).toEqual({ email: "newcomer@example.com", note: "We met at the market." });
  });

  // An invitation with no note carries no note, rather than an empty one.
  it("leaves the note out entirely when none was written", async () => {
    const sent = stubCreate();
    renderDialog();

    fireEvent.change(emailField(), { target: { value: "newcomer@example.com" } });
    fireEvent.click(sendButton());

    await waitFor(() => expect(sent).toHaveLength(1));
    expect(sent[0]).toEqual({ email: "newcomer@example.com" });
  });
});

describe("InviteDialog after sending", () => {
  async function send() {
    fireEvent.change(emailField(), { target: { value: "newcomer@example.com" } });
    fireEvent.click(sendButton());
    return screen.findByText(INVITE_DIALOG.readyTitle);
  }

  it("shows the link, and says the email is on its way", async () => {
    stubCreate();
    renderDialog();

    await send();

    expect(screen.getByText(INVITE_DIALOG.emailSent)).toBeInTheDocument();
    expect(screen.getByLabelText(INVITE_DIALOG.linkLabel)).toHaveValue(
      "https://bell.example.test/auth/registration?invite=tok-abc",
    );
  });

  // The invitation is fine; the town's mail is not. The sender has to be told
  // the difference, because only one of the two leaves them with a job to do.
  it("asks the sender to pass the link on when the email did not go", async () => {
    stubCreate({
      response: created({ email_sent: false, email_error: "dial tcp 127.0.0.1:587: refused" }),
    });
    renderDialog();

    await send();

    expect(screen.getByText(INVITE_DIALOG.emailFailed)).toBeInTheDocument();
    expect(screen.getByText(/dial tcp 127.0.0.1:587: refused/)).toBeInTheDocument();
  });

  it("says nothing about mail errors when the mail went", async () => {
    stubCreate();
    renderDialog();

    await send();

    expect(screen.queryByText(/refused/)).not.toBeInTheDocument();
  });

  it("copies the link to the clipboard", async () => {
    const writeText = vi.fn(() => Promise.resolve());
    vi.stubGlobal("navigator", { clipboard: { writeText } });
    stubCreate();
    renderDialog();

    await send();
    fireEvent.click(screen.getByRole("button", { name: INVITE_DIALOG.copy }));

    await waitFor(() => expect(writeText).toHaveBeenCalledWith(
      "https://bell.example.test/auth/registration?invite=tok-abc",
    ));
    expect(await screen.findByText(INVITE_DIALOG.copiedNotice)).toBeInTheDocument();
  });

  // navigator.clipboard is absent outside a secure context. Selecting the text
  // leaves the sender one keystroke away rather than stranded — and, crucially,
  // they are not told it was copied when it was not.
  it("selects the link instead when there is no clipboard", async () => {
    vi.stubGlobal("navigator", {});
    const select = vi.spyOn(HTMLInputElement.prototype, "select");
    stubCreate();
    renderDialog();

    await send();
    fireEvent.click(screen.getByRole("button", { name: INVITE_DIALOG.copy }));

    expect(await screen.findByText(INVITE_DIALOG.selected)).toBeInTheDocument();
    expect(select).toHaveBeenCalled();
    expect(screen.queryByText(INVITE_DIALOG.copiedNotice)).not.toBeInTheDocument();
  });

  it("tells the list something was created", async () => {
    stubCreate();
    const onInvited = vi.fn();
    renderDialog(onInvited);

    await send();

    expect(onInvited).toHaveBeenCalled();
  });

  // Closing on 201 would throw away the only copy of the token there will ever
  // be.
  it("stays open until the sender closes it", async () => {
    stubCreate();
    const onClose = vi.fn();
    renderDialog(() => {}, onClose);

    await send();
    expect(onClose).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: INVITE_DIALOG.done }));
    expect(onClose).toHaveBeenCalled();
  });
});

describe("InviteDialog refusals", () => {
  it("passes the server's already-invited refusal through", async () => {
    stubCreate({
      fail: { status: 409, error: "validation error: that neighbour has already been invited" },
    });
    renderDialog();

    fireEvent.change(emailField(), { target: { value: "newcomer@example.com" } });
    fireEvent.click(sendButton());

    expect(
      await screen.findByText("That neighbour has already been invited."),
    ).toBeInTheDocument();
  });

  it("names the shared daily budget when today's is spent", async () => {
    stubCreate({ fail: { status: 429, error: "rate limit exceeded" } });
    renderDialog();

    fireEvent.change(emailField(), { target: { value: "newcomer@example.com" } });
    fireEvent.click(sendButton());

    expect(await screen.findByText(/share a budget of 3 a day/)).toBeInTheDocument();
  });

  // A refused invitation leaves the form as it was, so the address does not
  // have to be typed again to retry.
  it("keeps the form, and what was typed into it, after a refusal", async () => {
    stubCreate({ fail: { status: 500, error: "internal error" } });
    renderDialog();

    fireEvent.change(emailField(), { target: { value: "newcomer@example.com" } });
    fireEvent.click(sendButton());

    await screen.findByText(/could not be sent/);
    expect(emailField()).toHaveValue("newcomer@example.com");
    expect(screen.queryByText(INVITE_DIALOG.readyTitle)).not.toBeInTheDocument();
  });
});
