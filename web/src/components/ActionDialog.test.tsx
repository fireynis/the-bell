import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { User } from "../api/types";
import { BAN_IS_COUNCIL_ONLY } from "../lib/moderation";
import ActionDialog from "./ActionDialog";

/**
 * Who the dialog offers a ban to.
 *
 * authorizeAction in internal/service/moderation_action.go reserves it for the
 * council: a ban is permanent, it zeroes the target's trust and it takes points
 * from everyone who vouched for them, three hops out. The dialog used to offer
 * it to any moderator, who would choose it, describe the offence, and get back
 * the word "forbidden".
 */

const COUNCIL: Pick<User, "id" | "role" | "is_active"> = {
  id: "0193a7b2-aaaa-7000-8000-000000000001",
  role: "council",
  is_active: true,
};

const MODERATOR: Pick<User, "id" | "role" | "is_active"> = {
  ...COUNCIL,
  role: "moderator",
};

const TARGET = "0193a7b2-bbbb-7000-8000-000000000002";

function stubFetch() {
  const fetchMock = vi.fn(() =>
    Promise.resolve({
      ok: true,
      status: 201,
      json: () => Promise.resolve({}),
    } as unknown as Response),
  );
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

function renderDialog(moderator: Pick<User, "id" | "role" | "is_active"> | null) {
  const onActionTaken = vi.fn();
  render(
    <ActionDialog
      targetUserId={TARGET}
      moderator={moderator}
      onClose={() => {}}
      onActionTaken={onActionTaken}
    />,
  );
  return { onActionTaken };
}

/** The <option> for one action type, whatever suffix its label carries. */
const option = (name: RegExp) => screen.getByRole("option", { name }) as HTMLOptionElement;

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("ActionDialog ban authorization", () => {
  it("offers a council member the ban", () => {
    renderDialog(COUNCIL);
    expect(option(/^Ban$/).disabled).toBe(false);
  });

  it("disables it for an ordinary moderator", () => {
    renderDialog(MODERATOR);
    expect(option(/^Ban/).disabled).toBe(true);
  });

  // Disabled rather than hidden, and with the rule beside it: a moderator who
  // needs somebody banned has to know it is the council they must ask.
  it("says who can ban instead of quietly dropping the option", () => {
    renderDialog(MODERATOR);
    expect(screen.getByText(BAN_IS_COUNCIL_ONLY)).toBeInTheDocument();
  });

  it("says nothing about it to the council", () => {
    renderDialog(COUNCIL);
    expect(screen.queryByText(BAN_IS_COUNCIL_ONLY)).toBeNull();
  });

  // A viewer whose own profile has not loaded is not known to be council.
  it("withholds the ban while the viewer is unknown", () => {
    renderDialog(null);
    expect(option(/^Ban/).disabled).toBe(true);
  });

  it.each(["Warn", "Mute", "Suspend"])("leaves %s available to a moderator", (label) => {
    renderDialog(MODERATOR);
    expect(option(new RegExp(`^${label}`)).disabled).toBe(false);
  });
});

describe("ActionDialog request", () => {
  // The rank rule changes who may ban and nothing else about what is sent.
  it("sends the same body for an action a moderator may take", async () => {
    const fetchMock = stubFetch();
    renderDialog(MODERATOR);

    fireEvent.change(screen.getByLabelText("Action Type"), { target: { value: "mute" } });
    fireEvent.change(screen.getByLabelText("Reason"), { target: { value: "Repeated spam" } });
    fireEvent.change(screen.getByLabelText("Duration (hours)"), { target: { value: "24" } });
    fireEvent.click(screen.getByRole("button", { name: "Take Action" }));

    await waitFor(() => expect(fetchMock).toHaveBeenCalled());

    const [url, init] = fetchMock.mock.calls[0] as unknown as [string, RequestInit];
    expect(url).toBe("/api/v1/moderation/actions");
    expect(JSON.parse(init.body as string)).toEqual({
      target_user_id: TARGET,
      action_type: "mute",
      severity: 3,
      reason: "Repeated spam",
      duration_seconds: 86400,
    });
  });

  it("lets the council send a ban", async () => {
    const fetchMock = stubFetch();
    renderDialog(COUNCIL);

    fireEvent.change(screen.getByLabelText("Action Type"), { target: { value: "ban" } });
    fireEvent.change(screen.getByLabelText("Reason"), { target: { value: "Harassment" } });
    fireEvent.click(screen.getByRole("button", { name: "Take Action" }));

    await waitFor(() => expect(fetchMock).toHaveBeenCalled());

    const [, init] = fetchMock.mock.calls[0] as unknown as [string, RequestInit];
    expect(JSON.parse(init.body as string)).toEqual({
      target_user_id: TARGET,
      action_type: "ban",
      severity: 5,
      reason: "Harassment",
    });
  });
});
