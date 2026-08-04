import { describe, expect, it } from "vitest";
import { deriveThemeVars, hexToHSL } from "./color";

describe("hexToHSL", () => {
  it("converts a six-digit colour", () => {
    expect(hexToHSL("#3b82f6")).toEqual({ h: 217, s: 91, l: 60 });
  });

  it("accepts a colour without the leading hash", () => {
    expect(hexToHSL("3b82f6")).toEqual(hexToHSL("#3b82f6"));
  });

  it("accepts uppercase digits", () => {
    expect(hexToHSL("#3B82F6")).toEqual(hexToHSL("#3b82f6"));
  });

  // #abc is the CSS shorthand for #aabbcc, and an admin may well paste one.
  it("expands three-digit shorthand the way CSS does", () => {
    expect(hexToHSL("#abc")).toEqual(hexToHSL("#aabbcc"));
    expect(hexToHSL("f00")).toEqual(hexToHSL("#ff0000"));
  });

  it("ignores surrounding whitespace from a pasted value", () => {
    expect(hexToHSL("  #3b82f6  ")).toEqual(hexToHSL("#3b82f6"));
  });

  // A greyscale colour has no chroma to take a hue from; both must read zero
  // rather than dividing by it.
  it.each([
    ["#000000", { h: 0, s: 0, l: 0 }],
    ["#ffffff", { h: 0, s: 0, l: 100 }],
    ["#808080", { h: 0, s: 0, l: 50 }],
  ])("reports %s as a hueless grey", (hex, want) => {
    expect(hexToHSL(hex)).toEqual(want);
  });

  it("places a red hue on the branch that wraps past 360", () => {
    expect(hexToHSL("#ff0000")?.h).toBe(0);
    // Slightly more blue than green, so (g - b) is negative and the hue wraps.
    expect(hexToHSL("#ff0010")?.h).toBe(356);
  });

  it("places a green hue in the middle of the circle", () => {
    expect(hexToHSL("#00ff00")?.h).toBe(120);
  });

  it("places a blue hue on the last branch", () => {
    expect(hexToHSL("#0000ff")?.h).toBe(240);
  });

  it("reports full saturation for a pure colour and almost none for a near-grey", () => {
    expect(hexToHSL("#ff0000")?.s).toBe(100);
    expect(hexToHSL("#7d8080")?.s).toBe(1);
  });

  // The saturation formula switches denominator at the midpoint, so both sides
  // of it have to land on the same answer for the same colour.
  it("saturates a light tint and its dark twin equally", () => {
    expect(hexToHSL("#ff8080")?.s).toBe(100);
    expect(hexToHSL("#800000")?.s).toBe(100);
  });

  // A bad colour in town config must leave the stylesheet defaults alone
  // rather than blank the app.
  it.each([
    ["", "empty"],
    ["#12345", "five digits"],
    ["#1234567", "seven digits"],
    ["#gggggg", "non-hex letters"],
    ["rgb(1,2,3)", "a CSS function"],
    ["#ab", "two digits"],
    ["blue", "a colour keyword"],
  ])("returns null for %s (%s)", (input) => {
    expect(hexToHSL(input)).toBeNull();
  });

  it("returns null rather than throwing for a non-string", () => {
    expect(hexToHSL(null as unknown as string)).toBeNull();
    expect(hexToHSL(undefined as unknown as string)).toBeNull();
  });
});

describe("deriveThemeVars", () => {
  it("derives a hover shade and two tints from the primary colour", () => {
    const vars = deriveThemeVars("#3b82f6");
    expect(vars).toEqual({
      "--color-primary": "#3b82f6",
      "--color-primary-hover": "hsl(217, 91%, 50%)",
      "--color-primary-light": "hsl(217, 91%, 94%)",
      "--color-primary-subtle": "hsl(217, 91%, 97%)",
    });
  });

  it("derives a hover shade and one tint from the accent colour", () => {
    const vars = deriveThemeVars(undefined, "#3b82f6");
    expect(vars).toEqual({
      "--color-accent": "#3b82f6",
      "--color-accent-hover": "hsl(217, 91%, 52%)",
      "--color-accent-light": "hsl(217, 91%, 94%)",
    });
  });

  it("derives both when both are given", () => {
    const vars = deriveThemeVars("#3b82f6", "#f59e0b");
    expect(Object.keys(vars)).toHaveLength(7);
  });

  // Otherwise a black primary would produce a negative lightness, which CSS
  // drops silently and leaves the hover state unstyled.
  it("clamps a hover shade darker than black up to zero", () => {
    expect(deriveThemeVars("#000000")["--color-primary-hover"]).toBe("hsl(0, 0%, 0%)");
    expect(deriveThemeVars(undefined, "#000000")["--color-accent-hover"]).toBe("hsl(0, 0%, 0%)");
  });

  it("keeps the hover shade of the lightest colour inside the range", () => {
    expect(deriveThemeVars("#ffffff")["--color-primary-hover"]).toBe("hsl(0, 0%, 90%)");
  });

  it("passes the original hex through untouched, so the exact brand colour is used", () => {
    expect(deriveThemeVars("3b82f6")["--color-primary"]).toBe("3b82f6");
  });

  it("returns nothing when no colours are configured", () => {
    expect(deriveThemeVars()).toEqual({});
    expect(deriveThemeVars(undefined, undefined)).toEqual({});
  });

  it("returns nothing for an empty string, which is not a colour", () => {
    expect(deriveThemeVars("", "")).toEqual({});
  });

  // A bad accent must not cost the town its good primary.
  it("still applies the primary when the accent is unparseable", () => {
    const vars = deriveThemeVars("#3b82f6", "not-a-colour");
    expect(vars["--color-primary"]).toBe("#3b82f6");
    expect(vars["--color-accent"]).toBeUndefined();
  });

  it("still applies the accent when the primary is unparseable", () => {
    const vars = deriveThemeVars("nope", "#f59e0b");
    expect(vars["--color-primary"]).toBeUndefined();
    expect(vars["--color-accent"]).toBe("#f59e0b");
  });
});
