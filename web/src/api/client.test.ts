import { afterEach, describe, expect, it, vi } from "vitest";
import type { ApiError, Vouch } from "./types";
import { moderationApi, vouchApi } from "./client";

/**
 * These exercise the client against the response shapes the vouch handler
 * documents in internal/handler/vouch.go — 201 with the created vouch, 204 with
 * no body, and an error carrying the server's own message.
 *
 * They verify this client's handling of those shapes, not the wire contract
 * itself: fetch is stubbed, so a server that stopped honouring the contract
 * would not fail these. Only a running stack can prove the contract.
 */

interface StubResponse {
  ok: boolean;
  status: number;
  statusText?: string;
  /** Body parser; made to throw where the client must not call it. */
  json: () => Promise<unknown>;
}

function stubFetch(response: StubResponse) {
  const fetchMock = vi.fn(() => Promise.resolve(response as unknown as Response));
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

/** A 204 has no body at all; parsing one throws, as the real Response does. */
function noContent(): StubResponse {
  return {
    ok: true,
    status: 204,
    json: () => Promise.reject(new SyntaxError("Unexpected end of JSON input")),
  };
}

/**
 * What POST /api/v1/vouches answers with. domain.Vouch is tagged to match the
 * vouchEntry DTO the profile listing uses, so both endpoints describe a vouch
 * the same way; revoked_at is omitted while the vouch is active.
 */
const CREATED_VOUCH: Vouch = {
  id: "01930000-0000-7000-8000-000000000001",
  voucher_id: "voucher-1",
  vouchee_id: "vouchee-1",
  status: "active",
  created_at: "2026-08-05T01:00:00Z",
};

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("vouchApi.create", () => {
  it("posts the vouchee id to the vouches collection", async () => {
    const fetchMock = stubFetch({
      ok: true,
      status: 201,
      json: () => Promise.resolve(CREATED_VOUCH),
    });

    await vouchApi.create({ vouchee_id: "vouchee-1" });

    const [url, init] = fetchMock.mock.calls[0] as unknown as [string, RequestInit];
    expect(url).toBe("/api/v1/vouches");
    expect(init.method).toBe("POST");
    expect(JSON.parse(init.body as string)).toEqual({ vouchee_id: "vouchee-1" });
  });

  // chi routes both /v1/vouches and /v1/vouches/ to the same handler, so the
  // path without the trailing slash the Go tests use is still correct.
  it("sends credentials, since the handler reads the session user", async () => {
    const fetchMock = stubFetch({
      ok: true,
      status: 201,
      json: () => Promise.resolve(CREATED_VOUCH),
    });

    await vouchApi.create({ vouchee_id: "vouchee-1" });

    const [, init] = fetchMock.mock.calls[0] as unknown as [string, RequestInit];
    expect(init.credentials).toBe("include");
  });

  it("parses the created vouch out of a 201", async () => {
    stubFetch({ ok: true, status: 201, json: () => Promise.resolve(CREATED_VOUCH) });

    await expect(vouchApi.create({ vouchee_id: "vouchee-1" })).resolves.toEqual(CREATED_VOUCH);
  });

  // The create and list endpoints describe a vouch identically, so the id from
  // one can be handed straight to revoke, which takes a vouch id.
  it("returns a vouch whose id is what revoke will need", async () => {
    stubFetch({ ok: true, status: 201, json: () => Promise.resolve(CREATED_VOUCH) });

    const created = await vouchApi.create({ vouchee_id: "vouchee-1" });
    expect(created.id).toBe(CREATED_VOUCH.id);
  });

  // revoked_at is `*time.Time` with omitempty, so on an active vouch the key is
  // absent altogether rather than present-and-null. Typing it optional rather
  // than nullable is what makes that the expected shape.
  it("omits revoked_at entirely while the vouch is active", async () => {
    stubFetch({ ok: true, status: 201, json: () => Promise.resolve(CREATED_VOUCH) });

    const created = await vouchApi.create({ vouchee_id: "v" });
    expect(created).not.toHaveProperty("revoked_at");
  });

  it("carries revoked_at once the vouch has been revoked", async () => {
    const revoked: Vouch = {
      ...CREATED_VOUCH,
      status: "revoked",
      revoked_at: "2026-08-06T00:00:00Z",
    };
    stubFetch({ ok: true, status: 201, json: () => Promise.resolve(revoked) });

    await expect(vouchApi.create({ vouchee_id: "v" })).resolves.toEqual(revoked);
  });
});

describe("vouchApi.revoke", () => {
  it("deletes the vouch by its own id", async () => {
    const fetchMock = stubFetch(noContent());

    await vouchApi.revoke("vouch-1");

    const [url, init] = fetchMock.mock.calls[0] as unknown as [string, RequestInit];
    expect(url).toBe("/api/v1/vouches/vouch-1");
    expect(init.method).toBe("DELETE");
  });

  // The handler answers 204 with no body. Parsing that throws, so the client
  // has to short-circuit before it reaches res.json().
  it("resolves on a 204 without trying to parse the empty body", async () => {
    const response = noContent();
    const jsonSpy = vi.spyOn(response, "json");
    stubFetch(response);

    await expect(vouchApi.revoke("vouch-1")).resolves.toBeUndefined();
    expect(jsonSpy).not.toHaveBeenCalled();
  });
});

describe("a refusal from the server", () => {
  // The server refuses for reasons the client cannot predict — a cycle in the
  // trust graph, the daily limit, the vouchee's state. Its wording is the only
  // accurate one, so it has to survive the trip to the component.
  it.each([
    [400, "cannot vouch for yourself"],
    [400, "daily vouch limit (3) reached"],
    [400, "vouch would create a cycle in the trust graph"],
    [400, "vouch already exists for this pair"],
    [403, "voucher does not meet trust requirements"],
  ])("surfaces the %d message %o verbatim", async (status, message) => {
    stubFetch({
      ok: false,
      status,
      json: () => Promise.resolve({ error: message }),
    });

    const err = await vouchApi.create({ vouchee_id: "vouchee-1" }).catch((e: ApiError) => e);

    expect(err).toEqual({ error: message, status });
  });

  it("surfaces a refusal to revoke too", async () => {
    stubFetch({
      ok: false,
      status: 403,
      json: () => Promise.resolve({ error: "forbidden" }),
    });

    const err = await vouchApi.revoke("vouch-1").catch((e: ApiError) => e);
    expect(err).toEqual({ error: "forbidden", status: 403 });
  });

  // Without a parseable body there is no server wording to show, so the status
  // text stands in rather than the client inventing one.
  it("falls back to the status text when the error body is not JSON", async () => {
    stubFetch({
      ok: false,
      status: 502,
      statusText: "Bad Gateway",
      json: () => Promise.reject(new SyntaxError("not JSON")),
    });

    const err = await vouchApi.create({ vouchee_id: "v" }).catch((e: ApiError) => e);
    expect(err).toEqual({ error: "Bad Gateway", status: 502 });
  });
});

/**
 * POST /api/v1/moderation/posts/{id}/remove answers 204 with no body. The
 * removal reason is a moderator's private note that the server deliberately
 * never serializes, so there is nothing to parse and nothing to read back.
 */
describe("moderationApi.removePost", () => {
  it("posts the reason to the removal endpoint for that post", async () => {
    const fetchMock = stubFetch(noContent());

    await moderationApi.removePost("post-1", "harassment");

    const [url, init] = fetchMock.mock.calls[0] as unknown as [string, RequestInit];
    expect(url).toBe("/api/v1/moderation/posts/post-1/remove");
    expect(init.method).toBe("POST");
    expect(JSON.parse(init.body as string)).toEqual({ reason: "harassment" });
  });

  it("resolves on a 204 without trying to parse the empty body", async () => {
    const response = noContent();
    const jsonSpy = vi.spyOn(response, "json");
    stubFetch(response);

    await expect(moderationApi.removePost("post-1", "spam")).resolves.toBeUndefined();
    expect(jsonSpy).not.toHaveBeenCalled();
  });

  // The server's wording is the accurate one — "post is already removed" when
  // another moderator got there first, or the role refusal — so it has to reach
  // the card rather than being replaced with a generic message.
  it.each([
    [400, "validation: post is already removed"],
    [400, "validation: reason must not be empty"],
    [403, "forbidden"],
    [404, "not found"],
  ])("surfaces the %d message %o verbatim", async (status, message) => {
    stubFetch({
      ok: false,
      status,
      json: () => Promise.resolve({ error: message }),
    });

    const err = await moderationApi.removePost("post-1", "spam").catch((e: ApiError) => e);

    expect(err).toEqual({ error: message, status });
  });
});

/**
 * PATCH /api/v1/moderation/reports/{id} is what resolves a report. It is
 * pinned here because the failure mode it guards against is silent: the queue
 * used to drop a report from local state and never tell the server, so the
 * report returned `pending` on the next load.
 */
describe("moderationApi.updateReportStatus", () => {
  it("patches the report to reviewed", async () => {
    const fetchMock = stubFetch({
      ok: true,
      status: 200,
      json: () => Promise.resolve({ id: "report-1", status: "reviewed" }),
    });

    await moderationApi.updateReportStatus("report-1", "reviewed");

    const [url, init] = fetchMock.mock.calls[0] as unknown as [string, RequestInit];
    expect(url).toBe("/api/v1/moderation/reports/report-1");
    expect(init.method).toBe("PATCH");
    expect(JSON.parse(init.body as string)).toEqual({ status: "reviewed" });
  });

  it("surfaces a refusal rather than resolving", async () => {
    stubFetch({
      ok: false,
      status: 500,
      json: () => Promise.resolve({ error: "internal error" }),
    });

    const err = await moderationApi
      .updateReportStatus("report-1", "reviewed")
      .catch((e: ApiError) => e);

    expect(err).toEqual({ error: "internal error", status: 500 });
  });
});

/**
 * The un-mute endpoint is a DELETE of the mute itself, and answers 204 with no
 * body — including for a user who was not muted. The no-body half is what these
 * pin: json() is made to throw, as the real Response does on an empty body, so
 * a client that tried to parse one would fail here rather than in a moderator's
 * browser.
 */
describe("moderationApi.liftMute", () => {
  it("deletes the mute and does not parse the empty body", async () => {
    const fetchMock = stubFetch(noContent());

    await expect(moderationApi.liftMute("user-1")).resolves.toBeUndefined();

    const [url, init] = fetchMock.mock.calls[0] as unknown as [string, RequestInit];
    expect(url).toBe("/api/v1/moderation/users/user-1/mute");
    expect(init.method).toBe("DELETE");
  });

  it("surfaces the server's refusal", async () => {
    stubFetch({
      ok: false,
      status: 403,
      json: () => Promise.resolve({ error: "forbidden" }),
    });

    const err = await moderationApi.liftMute("user-1").catch((e: ApiError) => e);

    expect(err).toEqual({ error: "forbidden", status: 403 });
  });
});

describe("moderationApi.getMuteStatus", () => {
  it("reads the mute of the user named in the path", async () => {
    const fetchMock = stubFetch({
      ok: true,
      status: 200,
      json: () => Promise.resolve({ muted_until: "2026-08-12T12:00:00Z" }),
    });

    await expect(moderationApi.getMuteStatus("user-1")).resolves.toEqual({
      muted_until: "2026-08-12T12:00:00Z",
    });

    const [url, init] = fetchMock.mock.calls[0] as unknown as [string, RequestInit];
    expect(url).toBe("/api/v1/moderation/users/user-1/mute");
    expect(init.method ?? "GET").toBe("GET");
  });

  it("reads an unmuted user as an object with no expiry at all, not a null", async () => {
    stubFetch({ ok: true, status: 200, json: () => Promise.resolve({}) });

    await expect(moderationApi.getMuteStatus("user-1")).resolves.toEqual({});
  });
});

/**
 * The suspension read and lift, whose shapes are the mute's one severity up:
 * `suspended_until` absent rather than null when nothing is in force, and a 204
 * with no body from the DELETE whether or not the user was suspended.
 */
describe("moderationApi.getSuspensionStatus", () => {
  it("reads the suspension of the user named in the path", async () => {
    const fetchMock = stubFetch({
      ok: true,
      status: 200,
      json: () => Promise.resolve({ suspended_until: "2026-08-12T12:00:00Z" }),
    });

    await expect(moderationApi.getSuspensionStatus("user-1")).resolves.toEqual({
      suspended_until: "2026-08-12T12:00:00Z",
    });

    const [url, init] = fetchMock.mock.calls[0] as unknown as [string, RequestInit];
    expect(url).toBe("/api/v1/moderation/users/user-1/suspension");
    expect(init.method ?? "GET").toBe("GET");
  });

  it("reads an unsuspended user as an object with no expiry at all, not a null", async () => {
    stubFetch({ ok: true, status: 200, json: () => Promise.resolve({}) });

    await expect(moderationApi.getSuspensionStatus("user-1")).resolves.toEqual({});
  });
});

describe("moderationApi.liftSuspension", () => {
  it("deletes the suspension and does not parse the empty body", async () => {
    const fetchMock = stubFetch(noContent());

    await expect(moderationApi.liftSuspension("user-1")).resolves.toBeUndefined();

    const [url, init] = fetchMock.mock.calls[0] as unknown as [string, RequestInit];
    expect(url).toBe("/api/v1/moderation/users/user-1/suspension");
    expect(init.method).toBe("DELETE");
  });

  // The server refuses a moderator lifting their own with a 400, which the UI
  // avoids reaching but must still report honestly if it does.
  it("surfaces the server's refusal", async () => {
    stubFetch({
      ok: false,
      status: 400,
      json: () => Promise.resolve({ error: "cannot moderate yourself" }),
    });

    const err = await moderationApi.liftSuspension("user-1").catch((e: ApiError) => e);

    expect(err).toEqual({ error: "cannot moderate yourself", status: 400 });
  });
});
