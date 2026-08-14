import { render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { OwnModerationEntry } from "../api/types";
import OwnModerationHistory from "./OwnModerationHistory";

/**
 * The member's own moderation record on screen.
 *
 * These pin two things a passing type-check cannot: that a member is told what
 * happened to them in words they did not have to learn, and that nothing on the
 * page attributes the decision to a person.
 */

const answer = (body: unknown, ok = true, status = 200) =>
  Promise.resolve({ ok, status, json: () => Promise.resolve(body) } as unknown as Response);

/** Stubs the one request the component makes, and records what it asked for. */
function stubHistory(actions: OwnModerationEntry[], failWith?: number) {
  const calls: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn((url: string) => {
      calls.push(url);
      if (failWith) return answer({ error: "internal error" }, false, failWith);
      return answer({ actions });
    }),
  );
  return calls;
}

function entry(overrides: Partial<OwnModerationEntry> = {}): OwnModerationEntry {
  return {
    id: "act-1",
    action: "warn",
    severity: 1,
    reason: "posting the same thing repeatedly",
    created_at: "2026-03-01T12:00:00Z",
    ...overrides,
  };
}

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("OwnModerationHistory", () => {
  it("tells a warned member what happened, why, and what it cost", async () => {
    stubHistory([
      entry({ penalty: { amount: 5, decays_at: "2026-05-30T12:00:00Z" } }),
    ]);

    render(<OwnModerationHistory />);

    expect(await screen.findByText("You were warned — minor")).toBeTruthy();
    expect(screen.getByText("posting the same thing repeatedly")).toBeTruthy();
    expect(
      screen.getByText("This cost 5 trust points; fully faded by May 2026."),
    ).toBeTruthy();
  });

  it("names nobody", async () => {
    // The response carries no moderator. This is the rendering half of the
    // same promise: nothing on screen may attribute the decision to a person.
    stubHistory([entry({ action: "suspend", severity: 4, expires_at: "2026-03-08T12:00:00Z" })]);

    render(<OwnModerationHistory />);

    const list = await screen.findByTestId("own-moderation-history");
    expect(list.textContent?.toLowerCase()).not.toContain("moderator");
  });

  it("greets a clean record as the normal state rather than an absence", async () => {
    stubHistory([]);

    render(<OwnModerationHistory />);

    expect(
      await screen.findByText("Nothing here — and that's how it stays for most people."),
    ).toBeTruthy();
  });

  it("says when a live restriction ends", async () => {
    stubHistory([
      entry({ action: "mute", severity: 3, expires_at: "2099-03-04T12:00:00Z" }),
    ]);

    render(<OwnModerationHistory />);

    expect(await screen.findByText(/^This ends on /)).toBeTruthy();
  });

  it("offers a retry when the read fails, instead of an empty record", async () => {
    // Reporting "nothing here" when the truth is unknown would tell a member
    // their record is clean on the strength of a failed request.
    const calls = stubHistory([], 500);

    render(<OwnModerationHistory />);

    expect(await screen.findByText("internal error")).toBeTruthy();
    expect(screen.queryByText(/that's how it stays/)).toBeNull();
    expect(calls).toHaveLength(1);
  });

  it("asks the server for one page and names no user in the request", async () => {
    // There is no id in the URL, which is exactly why there is no way to ask
    // this endpoint about anybody else.
    const calls = stubHistory([]);

    render(<OwnModerationHistory />);

    await waitFor(() => expect(calls).toHaveLength(1));
    expect(calls[0]).toBe("/api/v1/users/me/moderation-history?limit=20&offset=0");
  });
});
