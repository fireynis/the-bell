import { describe, expect, it } from "vitest";
import { describeNeighborCount, roleLabel } from "./directory";

describe("roleLabel", () => {
  // The directory exists so newcomers can be found and welcomed, so a pending
  // member is not described with the vocabulary of a processing queue.
  it("greets a pending member rather than labelling them pending", () => {
    expect(roleLabel("pending")).toBe("New here");
    expect(roleLabel("pending").toLowerCase()).not.toContain("pending");
  });

  it.each([
    ["member", "Member"],
    ["moderator", "Moderator"],
    ["council", "Council"],
  ])("names %s plainly as %s", (role, want) => {
    expect(roleLabel(role)).toBe(want);
  });

  // The server can add a role this build has never seen; showing the word it
  // sent beats inventing one or showing none.
  it("shows an unknown role as it came, capitalised", () => {
    expect(roleLabel("archivist")).toBe("Archivist");
  });

  it("leaves an already-capitalised unknown role alone", () => {
    expect(roleLabel("Archivist")).toBe("Archivist");
  });

  // The row joins this to the joined date with a separator, so an empty string
  // has to mean "leave the whole thing out".
  it.each([
    ["an empty role", ""],
    ["whitespace", "   "],
    ["an absent role", undefined],
    ["a null role", null],
  ])("returns nothing for %s", (_label, role) => {
    expect(roleLabel(role)).toBe("");
  });
});

describe("describeNeighborCount", () => {
  it("says how many are on screen out of how many match", () => {
    expect(describeNeighborCount(25, 112, false)).toBe("Showing 25 of 112 neighbours");
  });

  // Once the list is complete there is no "of" to report, only the size of the
  // town.
  it("drops the 'showing' once everyone is loaded", () => {
    expect(describeNeighborCount(12, 12, false)).toBe("12 neighbours");
  });

  it("counts matches rather than neighbours while searching", () => {
    expect(describeNeighborCount(3, 3, true)).toBe("3 matches");
    expect(describeNeighborCount(10, 40, true)).toBe("Showing 10 of 40 matches");
  });

  it.each([
    [1, false, "1 neighbour"],
    [1, true, "1 match"],
  ])("says %d in the singular when searching is %s", (total, searching, want) => {
    expect(describeNeighborCount(total, total, searching)).toBe(want);
  });

  // The empty list carries its own message; "0 neighbours" beneath it would only
  // repeat the bad news.
  it.each([
    ["nothing matched", 0],
    ["the total has not arrived yet", null],
    ["the total is nonsense", Number.NaN],
  ])("says nothing when %s", (_label, total) => {
    expect(describeNeighborCount(0, total, false)).toBe("");
  });
});
