import { describe, expect, it } from "vitest";
import { personName, shortId } from "./people";

/**
 * The fallback is the whole point of these. A member who has set no display
 * name reaches the client as the empty string on the vouch listing and as an
 * absent key on the report and moderation-action listings, so anything that
 * falls back on `undefined` alone renders a row naming nobody.
 */

describe("personName", () => {
  it("uses the display name when there is one", () => {
    expect(personName("Ada Lovelace", "0193a7b2-1234")).toBe("Ada Lovelace");
  });

  // The three conventions the API actually uses for "no name set".
  it.each([
    ["an absent key, as the report and action listings send", undefined],
    ["an explicit null", null],
    ["the empty string, as the vouch listing sends", ""],
    ["a name that is only whitespace", "   "],
  ])("falls back to the short id for %s", (_label, name) => {
    expect(personName(name, "0193a7b2-1234")).toBe("0193a7b2...");
  });

  it("trims a name rather than rendering its padding", () => {
    expect(personName("  Ada  ", "0193a7b2-1234")).toBe("Ada");
  });

  it("never renders an empty name, even with nothing to fall back to", () => {
    expect(personName("", "")).not.toBe("");
  });
});

describe("shortId", () => {
  it("truncates a uuid to its first eight characters", () => {
    expect(shortId("0193a7b2-5f3e-7000-8000-000000000000")).toBe("0193a7b2...");
  });

  // Without the marker the eight characters read as a whole identifier, and two
  // truncated rows look like two complete ones.
  it("marks the truncation", () => {
    expect(shortId("0193a7b2-5f3e")).toContain("...");
  });

  // The marker claims something was cut off, so it must not appear when
  // nothing was.
  it("does not claim truncation for an id short enough to render whole", () => {
    expect(shortId("abc")).toBe("abc");
  });

  it.each([
    ["an empty id", ""],
    ["an absent id", undefined],
    ["a null id", null],
  ])("reads as unknown for %s rather than rendering nothing", (_label, id) => {
    expect(shortId(id)).toBe("unknown");
  });
});
