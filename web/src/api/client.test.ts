import { afterEach, describe, expect, it, vi } from "vitest";
import type { ApiError, Vouch } from "./types";
import { vouchApi } from "./client";

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

  it("returns a vouch whose id is what revoke will need", async () => {
    stubFetch({ ok: true, status: 201, json: () => Promise.resolve(CREATED_VOUCH) });

    const created = await vouchApi.create({ vouchee_id: "vouchee-1" });
    expect(created.id).toBe(CREATED_VOUCH.id);
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
