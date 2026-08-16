import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { ReactElement } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { MuteBanner, SuspensionBanner, type BannerProps } from "./RestrictionBanner";

/**
 * The server could lift both restrictions long before anything could reach
 * either. SetUserMutedUntil(ctx, id, nil) was tested from the day it was
 * written and nothing called it; LiftSuspension arrived with the same gap, and
 * until it existed a suspension could only end by lapsing. This banner is the
 * affordance that reaches them, and these pin the three things that make it
 * honest: it only appears when the restriction really is in force, it stops
 * claiming one once it is lifted, and it never claims one it could not read.
 *
 * A moderator's view of another user carries muted_until and suspended_until
 * nowhere else — both are absent from the public profile by design — so the
 * banner reads the live endpoints. fetch is stubbed here; the wire contract
 * belongs to internal/handler/moderation.go and its own tests.
 */

const FUTURE = "2099-08-12T12:00:00Z";
const PAST = "2020-08-12T12:00:00Z";

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

/**
 * One restriction as these tests know it: the descriptor the banner is given,
 * the wire shape its endpoint answers with, and the wording that belongs to it.
 */
interface Case {
  name: string;
  /** The banner under test, already bound to its restriction. */
  Banner: (props: BannerProps) => ReactElement;
  inForceBody: unknown;
  lapsedBody: unknown;
  /** The last path segment of both its endpoints. */
  path: string;
  headline: RegExp;
  liftLabel: RegExp;
  confirmTitle: RegExp;
  liftedNotice: RegExp;
}

const MUTE_CASE: Case = {
  name: "mute",
  Banner: MuteBanner,
  inForceBody: { muted_until: FUTURE },
  lapsedBody: { muted_until: PAST },
  path: "mute",
  headline: /currently muted until/i,
  liftLabel: /^lift mute$/i,
  confirmTitle: /lift this mute\?/i,
  liftedNotice: /mute lifted/i,
};

const SUSPENSION_CASE: Case = {
  name: "suspension",
  Banner: SuspensionBanner,
  inForceBody: { suspended_until: FUTURE },
  lapsedBody: { suspended_until: PAST },
  path: "suspension",
  headline: /currently suspended until/i,
  liftLabel: /^lift suspension$/i,
  confirmTitle: /lift this suspension\?/i,
  liftedNotice: /suspension lifted/i,
};

const ok = (body: unknown) => ({ ok: true, code: 200, body });

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

/**
 * Every behaviour here is asserted against both restrictions. The point of one
 * banner over two is that the mute and the suspension cannot diverge on the
 * idempotence, the self-lift refusal or the failure handling, and a suite that
 * ran against only one would leave exactly that room.
 */
function describeRestriction(c: Case) {
  describe(`RestrictionBanner: ${c.name}`, () => {
    const liftButton = () => screen.queryByRole("button", { name: c.liftLabel });
    const confirmButton = () =>
      screen.getAllByRole("button", { name: c.liftLabel }).at(-1) as HTMLElement;

    it("says when the restriction ends and offers to lift it", async () => {
      stubApi(ok(c.inForceBody));

      render(<c.Banner userId="target-1" viewerId="mod-1" />);

      expect(await screen.findByText(c.headline)).toBeInTheDocument();
      await waitFor(() => expect(liftButton()).toBeEnabled());
    });

    it("shows nothing at all for a user who is not restricted", async () => {
      const calls = stubApi(ok({}));

      const { container } = render(
        <c.Banner userId="target-1" viewerId="mod-1" />,
      );

      await waitFor(() => expect(calls).toHaveLength(1));
      await waitFor(() => expect(container).toBeEmptyDOMElement());
    });

    // The server already reports a lapsed restriction as none at all, but a page
    // left open outlives the response that loaded it — so the expiry is re-checked
    // against the clock rather than trusted to still be in the future.
    it("shows nothing for a restriction whose time has already passed", async () => {
      stubApi(ok(c.lapsedBody));

      const { container } = render(
        <c.Banner userId="target-1" viewerId="mod-1" />,
      );

      await waitFor(() => expect(container).toBeEmptyDOMElement());
    });

    // Lifting cannot be undone in place: re-imposing the restriction is a fresh
    // action carrying a fresh trust penalty, for the member and for everyone who
    // vouched for them. The confirm says so before the moderator commits.
    it("asks before lifting, spelling out what the release does and costs", async () => {
      const calls = stubApi(ok(c.inForceBody));
      render(<c.Banner userId="target-1" viewerId="mod-1" />);
      await waitFor(() => expect(liftButton()).toBeEnabled());

      fireEvent.click(liftButton()!);

      expect(await screen.findByRole("dialog")).toBeInTheDocument();
      expect(screen.getByText(c.confirmTitle)).toBeInTheDocument();
      expect(screen.getByText(/immediately/i)).toBeInTheDocument();
      expect(screen.getByText(/their own profile/i)).toBeInTheDocument();
      expect(screen.getByText(/costs them no trust/i)).toBeInTheDocument();
      expect(screen.getByText(/not filed as an action/i)).toBeInTheDocument();
      // Nothing has reached the server on the strength of one click.
      expect(calls.filter((call) => call.method === "DELETE")).toHaveLength(0);
    });

    it("does not lift when the confirm is declined", async () => {
      const calls = stubApi(ok(c.inForceBody));
      render(<c.Banner userId="target-1" viewerId="mod-1" />);
      await waitFor(() => expect(liftButton()).toBeEnabled());

      fireEvent.click(liftButton()!);
      fireEvent.click(await screen.findByRole("button", { name: /cancel/i }));

      await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
      expect(calls.filter((call) => call.method === "DELETE")).toHaveLength(0);
      expect(screen.getByText(c.headline)).toBeInTheDocument();
    });

    it("lifts on the server and stops claiming the user is restricted", async () => {
      const calls = stubApi(ok(c.inForceBody));
      render(<c.Banner userId="target-1" viewerId="mod-1" />);
      await waitFor(() => expect(liftButton()).toBeEnabled());

      fireEvent.click(liftButton()!);
      fireEvent.click(confirmButton());

      await waitFor(() =>
        expect(calls).toContainEqual({
          url: `/api/v1/moderation/users/target-1/${c.path}`,
          method: "DELETE",
        }),
      );
      expect(await screen.findByText(c.liftedNotice)).toBeInTheDocument();
      expect(screen.queryByText(c.headline)).not.toBeInTheDocument();
    });

    // The lift answers 204 with no body, for a restriction in force and for one
    // that was not. The banner takes that as the whole answer instead of re-reading
    // the status, so an empty body is not a parse failure and one GET is all there
    // ever is.
    it("takes the empty 204 as the answer without a second read", async () => {
      const calls = stubApi(ok(c.inForceBody));
      render(<c.Banner userId="target-1" viewerId="mod-1" />);
      await waitFor(() => expect(liftButton()).toBeEnabled());

      fireEvent.click(liftButton()!);
      fireEvent.click(confirmButton());

      expect(await screen.findByText(c.liftedNotice)).toBeInTheDocument();
      expect(calls.filter((call) => call.method === "GET")).toHaveLength(1);
    });

    // The restriction is still in force after a failed DELETE, so the banner has
    // to stay: a control that clears itself on failure tells the moderator the
    // appeal is handled while the member is still restricted.
    it("keeps the banner and reports the failure when the lift is refused", async () => {
      stubApi(ok(c.inForceBody), { ok: false, code: 500, body: { error: "internal error" } });
      render(<c.Banner userId="target-1" viewerId="mod-1" />);
      await waitFor(() => expect(liftButton()).toBeEnabled());

      fireEvent.click(liftButton()!);
      fireEvent.click(confirmButton());

      expect(await screen.findByText(/internal error/i)).toBeInTheDocument();
      // Still open, so the moderator can try again in place, and still true.
      expect(screen.getByRole("dialog")).toBeInTheDocument();
      expect(screen.getByText(c.headline)).toBeInTheDocument();
    });

    // Mirrors canLiftRestriction in internal/service/moderation_action.go, which
    // refuses this outright — a restricted moderator satisfies every middleware in
    // the chain, so this is the one case no route guard can catch.
    it("will not offer a moderator the release of their own restriction", async () => {
      stubApi(ok(c.inForceBody));

      render(<c.Banner userId="mod-1" viewerId="mod-1" />);

      expect(await screen.findByText(c.headline)).toBeInTheDocument();
      await waitFor(() => expect(liftButton()).toBeDisabled());
      expect(screen.getByText(/cannot moderate yourself/i)).toBeInTheDocument();
    });

    // Silence would read as "not restricted", which is the one answer this must
    // never invent: neither expiry is on any other response a moderator can see,
    // so if this read fails there is nothing to fall back on.
    it("says so when it could not read the status, rather than implying there is none", async () => {
      stubApi({ ok: false, code: 500, body: { error: "internal error" } });

      render(<c.Banner userId="target-1" viewerId="mod-1" />);

      expect(await screen.findByText(/could not be read/i)).toBeInTheDocument();
      expect(liftButton()).not.toBeInTheDocument();
    });
  });
}

describeRestriction(MUTE_CASE);
describeRestriction(SUSPENSION_CASE);

describe("RestrictionBanner wording", () => {
  // The two confirms must not be interchangeable. A mute blocks posting alone;
  // a suspension is folded into is_active and meets middleware.RequireActive on
  // every guarded route, so lifting one hands back reacting, vouching and
  // reporting too — and a moderator agreeing to it should read that.
  it("names what a suspension hands back, which is more than a mute does", async () => {
    stubApi(ok({ suspended_until: FUTURE }));
    render(<SuspensionBanner userId="target-1" viewerId="mod-1" />);
    await waitFor(() => expect(screen.getByRole("button", { name: /lift suspension/i })).toBeEnabled());

    fireEvent.click(screen.getByRole("button", { name: /lift suspension/i }));

    expect(await screen.findByText(/post, react, vouch and report again/i)).toBeInTheDocument();
  });

  it("reads the suspension endpoint rather than inferring it from is_active", async () => {
    const calls = stubApi(ok({}));

    render(<SuspensionBanner userId="target-1" viewerId="mod-1" />);

    await waitFor(() =>
      expect(calls).toContainEqual({
        url: "/api/v1/moderation/users/target-1/suspension",
        method: "GET",
      }),
    );
  });
});
