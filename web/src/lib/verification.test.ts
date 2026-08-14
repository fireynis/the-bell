import { describe, expect, it } from "vitest";
import type { RouteObject } from "react-router";
import {
  UNVERIFIED_EMAIL_NOTICE,
  VERIFICATION_PATH,
  apiErrorMessage,
  isEmailUnverified,
} from "./verification";
import { routes } from "../routes";

/**
 * The client's half of middleware.RequireVerifiedEmail.
 *
 * The guard skips GET /v1/me on purpose — somebody who may not participate
 * still has to be able to learn why — so an unverified member is signed in,
 * has a profile, and is refused everything else. Nothing here can be inferred
 * from the session; the refusal is the only evidence there is.
 */

const unverified = { error: "email not verified", status: 403 };

describe("isEmailUnverified", () => {
  it("recognizes the guard's refusal", () => {
    expect(isEmailUnverified(unverified)).toBe(true);
  });

  // The wording is what distinguishes it. Every other 403 tells a resident to
  // go and ask a moderator; this one tells them to open their inbox.
  it.each([
    ["forbidden", 403],
    ["account suspended", 403],
    ["council role required", 403],
    ["unauthorized", 401],
  ])("does not mistake %o (%i) for it", (error, status) => {
    expect(isEmailUnverified({ error, status })).toBe(false);
  });

  // A 200 body that happened to contain those words is not a refusal, and the
  // status is what says so.
  it("requires the 403, not just the wording", () => {
    expect(isEmailUnverified({ error: "email not verified", status: 200 })).toBe(false);
  });

  it("tolerates the whitespace a writer would not notice", () => {
    expect(isEmailUnverified({ error: " email not verified\n", status: 403 })).toBe(true);
  });

  // Catch clauses receive whatever was thrown, and a dropped connection throws
  // a TypeError rather than an ApiError.
  it.each([null, undefined, new TypeError("Failed to fetch"), "email not verified", 403])(
    "answers no for %o rather than throwing",
    (err) => {
      expect(isEmailUnverified(err)).toBe(false);
    },
  );
});

describe("apiErrorMessage", () => {
  it("replaces the guard's three words with something to act on", () => {
    expect(apiErrorMessage(unverified, "It did not work.")).toBe(UNVERIFIED_EMAIL_NOTICE);
  });

  // The server's own sentence is usually the most accurate one available — a
  // cycle in the trust graph, a daily limit — so it still wins everywhere else.
  it("keeps the server's wording for every other refusal", () => {
    expect(apiErrorMessage({ error: "daily vouch limit (3) reached", status: 400 }, "no")).toBe(
      "daily vouch limit (3) reached",
    );
  });

  it.each([null, undefined, {}, { status: 500 }, { error: "   ", status: 500 }])(
    "falls back to the caller's sentence for %o",
    (err) => {
      expect(apiErrorMessage(err, "Failed to load posts.")).toBe("Failed to load posts.");
    },
  );
});

describe("the notice", () => {
  it("says what to do rather than naming the rule", () => {
    expect(UNVERIFIED_EMAIL_NOTICE).toContain("inbox");
  });

  // The banner links here, so a path that no route serves would send a member
  // who is already stuck to a 404.
  it("points at a route the app actually serves", () => {
    const collect = (route: RouteObject): string[] => [
      route.path ?? "",
      ...(route.children ?? []).flatMap(collect),
    ];
    expect(routes.flatMap(collect)).toContain(VERIFICATION_PATH);
  });
});
