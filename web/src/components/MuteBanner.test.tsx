import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import MuteBanner from "./MuteBanner";

/**
 * SetUserMutedUntil(ctx, id, nil) lifts a mute and was tested from the day it
 * was written; nothing ever called it, so a mute applied in error ran its full
 * course. This is the affordance that reaches it, and these pin the two things
 * that make it honest: it only appears when the person really is muted, and it
 * stops claiming a mute once one is lifted.
 *
 * A moderator's view of another user carries muted_until nowhere else — it is
 * absent from the public profile by design — so the banner reads it from
 * GET /moderation/users/{id}/mute. fetch is stubbed here; the wire contract
 * belongs to internal/handler/moderation.go and its own tests.
 */

const MUTED_UNTIL = "2099-08-12T12:00:00Z";

interface Call {
  url: string;
  method: string;
}

/**
 * stubApi answers the status read with `status` and the lift with `liftStatus`,
 * recording every call so a test can assert the DELETE was actually issued —
 * the failure this whole feature exists to avoid is a control that appears to
 * work and never reaches the server.
 */
function stubApi(
  status: { ok: boolean; code: number; body: unknown },
  liftStatus: { ok: boolean; code: number; body?: unknown } = { ok: true, code: 204 },
) {
  const calls: Call[] = [];

  vi.stubGlobal(
    "fetch",
    vi.fn((url: string, init: RequestInit = {}) => {
      const method = init.method ?? "GET";
      calls.push({ url, method });

      if (method === "GET") {
        return Promise.resolve({
          ok: status.ok,
          status: status.code,
          json: () => Promise.resolve(status.body),
        } as unknown as Response);
      }
      return Promise.resolve({
        ok: liftStatus.ok,
        status: liftStatus.code,
        json: () =>
          liftStatus.body === undefined
            ? Promise.reject(new SyntaxError("Unexpected end of JSON input"))
            : Promise.resolve(liftStatus.body),
      } as unknown as Response);
    }),
  );

  return calls;
}

const mutedResponse = { ok: true, code: 200, body: { muted_until: MUTED_UNTIL } };
const unmutedResponse = { ok: true, code: 200, body: {} };

const liftButton = () => screen.queryByRole("button", { name: /lift mute/i });

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("MuteBanner", () => {
  it("says when the mute ends and offers to lift it", async () => {
    stubApi(mutedResponse);

    render(<MuteBanner userId="target-1" viewerId="mod-1" />);

    expect(await screen.findByText(/currently muted/i)).toBeInTheDocument();
    await waitFor(() => expect(liftButton()).toBeEnabled());
  });

  it("shows nothing at all for a user who is not muted", async () => {
    const calls = stubApi(unmutedResponse);

    const { container } = render(<MuteBanner userId="target-1" viewerId="mod-1" />);

    await waitFor(() => expect(calls).toHaveLength(1));
    await waitFor(() => expect(container).toBeEmptyDOMElement());
  });

  it("lifts the mute on the server and stops claiming the user is muted", async () => {
    const calls = stubApi(mutedResponse);
    render(<MuteBanner userId="target-1" viewerId="mod-1" />);
    await waitFor(() => expect(liftButton()).toBeEnabled());

    fireEvent.click(liftButton()!);

    await waitFor(() =>
      expect(calls).toContainEqual({
        url: "/api/v1/moderation/users/target-1/mute",
        method: "DELETE",
      }),
    );
    expect(await screen.findByText(/mute lifted/i)).toBeInTheDocument();
    expect(screen.queryByText(/currently muted/i)).not.toBeInTheDocument();
  });

  // The mute is still in force after a failed DELETE, so the banner has to stay:
  // a control that clears itself on failure tells the moderator the appeal is
  // handled when the member is still silenced.
  it("keeps the banner and reports the failure when the lift is refused", async () => {
    stubApi(mutedResponse, { ok: false, code: 500, body: { error: "internal error" } });
    render(<MuteBanner userId="target-1" viewerId="mod-1" />);
    await waitFor(() => expect(liftButton()).toBeEnabled());

    fireEvent.click(liftButton()!);

    expect(await screen.findByText(/internal error/i)).toBeInTheDocument();
    expect(screen.getByText(/currently muted/i)).toBeInTheDocument();
  });

  // Mirrors canLiftMute in internal/service/moderation_action.go, which refuses
  // this outright — a muted moderator satisfies every middleware in the chain,
  // so this is the one case no route guard can catch.
  it("will not offer a moderator the release of their own mute", async () => {
    stubApi(mutedResponse);

    render(<MuteBanner userId="mod-1" viewerId="mod-1" />);

    expect(await screen.findByText(/currently muted/i)).toBeInTheDocument();
    await waitFor(() => expect(liftButton()).toBeDisabled());
    expect(screen.getByText(/cannot moderate yourself/i)).toBeInTheDocument();
  });

  // Silence would read as "not muted", which is the one answer this must never
  // invent: muted_until is on no other response a moderator can see, so if this
  // read fails there is nothing else to fall back on.
  it("says so when it could not read the mute status, rather than implying there is none", async () => {
    stubApi({ ok: false, code: 500, body: { error: "internal error" } });

    render(<MuteBanner userId="target-1" viewerId="mod-1" />);

    expect(await screen.findByText(/could not be read/i)).toBeInTheDocument();
    expect(liftButton()).not.toBeInTheDocument();
  });
});
