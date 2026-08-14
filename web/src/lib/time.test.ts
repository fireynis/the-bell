import { describe, expect, it } from "vitest";
import {
  formatAbsoluteTime,
  formatDateTime,
  formatMonthYear,
  formatRelativeTime,
} from "./time";

const NOW = Date.parse("2026-03-01T12:00:00Z");

/** ago returns an ISO timestamp the given number of milliseconds before NOW. */
function ago(ms: number): string {
  return new Date(NOW - ms).toISOString();
}

const SECOND = 1000;
const MINUTE = 60 * SECOND;
const HOUR = 60 * MINUTE;
const DAY = 24 * HOUR;

describe("formatRelativeTime", () => {
  it.each([
    ["0 seconds", 0, "just now"],
    ["30 seconds", 30 * SECOND, "just now"],
    ["59 seconds", 59 * SECOND, "just now"],
    ["exactly 1 minute", MINUTE, "1m"],
    ["45 minutes", 45 * MINUTE, "45m"],
    ["59 minutes", 59 * MINUTE, "59m"],
    ["exactly 1 hour", HOUR, "1h"],
    ["23 hours", 23 * HOUR, "23h"],
    ["exactly 1 day", DAY, "1d"],
    ["6 days", 6 * DAY, "6d"],
  ])("renders %s as %s", (_name, offset, want) => {
    expect(formatRelativeTime(ago(offset), { now: NOW })).toBe(want);
  });

  it("falls back to an absolute date at a week old", () => {
    const got = formatRelativeTime(ago(7 * DAY), { now: NOW });
    expect(got).not.toMatch(/^\d+[mhd]$/);
    expect(got).not.toBe("just now");
  });

  // Mild clock skew between client and server can put a timestamp slightly in
  // the future; showing "-1m" would look broken.
  it("clamps a future timestamp to 'just now'", () => {
    const future = new Date(NOW + 5 * MINUTE).toISOString();
    expect(formatRelativeTime(future, { now: NOW })).toBe("just now");
  });

  it("returns an empty string for an unparseable date", () => {
    expect(formatRelativeTime("not a date", { now: NOW })).toBe("");
    expect(formatRelativeTime("", { now: NOW })).toBe("");
  });

  it("truncates rather than rounds, so 119 seconds is 1m", () => {
    expect(formatRelativeTime(ago(119 * SECOND), { now: NOW })).toBe("1m");
  });

  describe("with the 'ago' suffix used by the moderation queue", () => {
    it.each([
      [5 * MINUTE, "5m ago"],
      [3 * HOUR, "3h ago"],
      [2 * DAY, "2d ago"],
    ])("renders offset %d as %s", (offset, want) => {
      expect(formatRelativeTime(ago(offset), { now: NOW, suffix: true })).toBe(want);
    });

    // "just now ago" would read badly, so the suffix applies only to the
    // numeric forms.
    it("leaves 'just now' unsuffixed", () => {
      expect(formatRelativeTime(ago(10 * SECOND), { now: NOW, suffix: true })).toBe("just now");
    });

    it("leaves the absolute fallback unsuffixed", () => {
      const got = formatRelativeTime(ago(30 * DAY), { now: NOW, suffix: true });
      expect(got).not.toContain("ago");
    });
  });

  it("defaults to the current time when none is given", () => {
    expect(formatRelativeTime(new Date().toISOString())).toBe("just now");
  });
});

describe("formatAbsoluteTime", () => {
  it("renders a parseable timestamp", () => {
    expect(formatAbsoluteTime("2026-03-01T12:00:00Z")).not.toBe("");
  });

  // "Invalid Date" in a tooltip is worse than an empty tooltip.
  it("returns an empty string for an unparseable date", () => {
    expect(formatAbsoluteTime("not a date")).toBe("");
    expect(formatAbsoluteTime("")).toBe("");
  });
});

describe("formatMonthYear", () => {
  it("names the month and the year", () => {
    const got = formatMonthYear("2026-03-01T12:00:00Z");
    expect(got).toContain("2026");
    expect(got).toMatch(/[A-Za-z]/);
  });

  // Which day somebody joined is not what a directory is for; printing it makes
  // a list of neighbours read like an audit log.
  it("leaves the day out", () => {
    expect(formatMonthYear("2026-03-17T12:00:00Z")).not.toContain("17");
  });

  it("tells two months of the same year apart", () => {
    expect(formatMonthYear("2026-03-15T12:00:00Z")).not.toBe(
      formatMonthYear("2026-08-15T12:00:00Z"),
    );
  });

  it("tells the same month of two years apart", () => {
    expect(formatMonthYear("2025-03-15T12:00:00Z")).not.toBe(
      formatMonthYear("2026-03-15T12:00:00Z"),
    );
  });

  it.each(["not a date", "", "0000-13-45"])(
    "returns an empty string for the unparseable input %o",
    (input) => {
      expect(formatMonthYear(input)).toBe("");
    },
  );

  it("agrees with formatDateTime about what is unparseable", () => {
    for (const input of ["not a date", "", "2026-03-01T12:00:00Z"]) {
      expect(formatMonthYear(input) === "").toBe(formatDateTime(input) === "");
    }
  });
});

describe("formatDateTime", () => {
  it("renders a parseable timestamp", () => {
    expect(formatDateTime("2026-03-01T12:00:00Z")).not.toBe("");
  });

  // The audit trail needs the time of day, not just the date: two actions on
  // the same user on the same day have to be told apart.
  it("includes the time of day, so same-day actions are distinguishable", () => {
    const morning = formatDateTime("2026-03-01T09:30:00Z");
    const evening = formatDateTime("2026-03-01T21:30:00Z");
    expect(morning).not.toBe(evening);
  });

  it("includes the year, since history outlives the current one", () => {
    expect(formatDateTime("2026-03-01T12:00:00Z")).toContain("2026");
  });

  // The moderation card used to render the literal "Invalid Date" here.
  it.each(["not a date", "", "0000-13-45"])(
    "returns an empty string for the unparseable input %o",
    (input) => {
      expect(formatDateTime(input)).toBe("");
    },
  );

  it("agrees with formatAbsoluteTime about what is unparseable", () => {
    for (const input of ["not a date", "", "2026-03-01T12:00:00Z"]) {
      expect(formatDateTime(input) === "").toBe(formatAbsoluteTime(input) === "");
    }
  });
});
