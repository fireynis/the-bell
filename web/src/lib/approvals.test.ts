import { describe, expect, it } from "vitest";
import { describeQueueSize, describeWaitingCount, describeWaitingTime } from "./approvals";

/**
 * The wording the approval queue uses. It is worth pinning because the queue is
 * where the town talks about people who are not yet in it: the difference
 * between "7 pending users" and "7 neighbours waiting" is the difference
 * between a work tray and a town.
 */

const HOUR = 60 * 60 * 1000;
const DAY = 24 * HOUR;
const NOW = Date.parse("2026-08-13T12:00:00Z");

/** A timestamp the given number of milliseconds before NOW. */
const ago = (ms: number) => new Date(NOW - ms).toISOString();

describe("describeQueueSize", () => {
  it("counts neighbours rather than users", () => {
    expect(describeQueueSize(7)).toBe("7 neighbours waiting");
  });

  it("says one neighbour rather than 1 neighbours", () => {
    expect(describeQueueSize(1)).toBe("1 neighbour waiting");
  });

  // The dashboard renders nothing at all rather than announcing an empty queue
  // every time a council member opens the Town Hall.
  it.each([
    ["nobody waiting", 0],
    ["not yet loaded", null],
    ["a nonsense total", Number.NaN],
  ])("says nothing for %s", (_label, total) => {
    expect(describeQueueSize(total)).toBe("");
  });
});

describe("describeWaitingCount", () => {
  it("says how much of the queue is on screen while there is more to come", () => {
    expect(describeWaitingCount(25, 112, false)).toBe("Showing 25 of 112 neighbours waiting");
  });

  it("drops the comparison once the whole queue is loaded", () => {
    expect(describeWaitingCount(4, 4, false)).toBe("4 neighbours waiting");
  });

  // Searching answers a different question, so it counts matches rather than
  // neighbours: "3 neighbours waiting" under a filtered list would misreport
  // the size of the queue.
  it("counts matches while searching", () => {
    expect(describeWaitingCount(2, 2, true)).toBe("2 matches");
    expect(describeWaitingCount(1, 1, true)).toBe("1 match");
  });

  it("says nothing when there is nothing to count", () => {
    expect(describeWaitingCount(0, 0, false)).toBe("");
    expect(describeWaitingCount(0, null, false)).toBe("");
  });
});

describe("describeWaitingTime", () => {
  it("counts the days somebody has been waiting", () => {
    expect(describeWaitingTime(ago(12 * DAY), NOW)).toBe("Waiting 12 days");
  });

  it("says one day rather than 1 days", () => {
    expect(describeWaitingTime(ago(DAY), NOW)).toBe("Waiting 1 day");
  });

  it("falls back to hours inside the first day", () => {
    expect(describeWaitingTime(ago(5 * HOUR), NOW)).toBe("Waiting 5 hours");
    expect(describeWaitingTime(ago(HOUR), NOW)).toBe("Waiting 1 hour");
  });

  // Nobody decides differently at 40 minutes than at 20, and a page left open
  // should not tick.
  it("does not count minutes", () => {
    expect(describeWaitingTime(ago(40 * 60 * 1000), NOW)).toBe("Waiting since just now");
  });

  // Mild clock skew between client and server, not a negative wait.
  it("clamps a join date in the future rather than counting backwards", () => {
    expect(describeWaitingTime(new Date(NOW + HOUR).toISOString(), NOW)).toBe(
      "Waiting since just now",
    );
  });

  it.each([
    ["an unparseable timestamp", "not a date"],
    ["an empty string", ""],
    ["a missing value", undefined],
  ])("says nothing for %s, so the caller can drop the line", (_label, value) => {
    expect(describeWaitingTime(value, NOW)).toBe("");
  });

  // Rounded down: eleven and a half days is eleven, not twelve.
  it("rounds down to the coarsest honest unit", () => {
    expect(describeWaitingTime(ago(11 * DAY + 20 * HOUR), NOW)).toBe("Waiting 11 days");
  });
});
