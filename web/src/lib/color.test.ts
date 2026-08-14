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

  it("treats the light scheme as the default, so existing callers are unchanged", () => {
    expect(deriveThemeVars("#3b82f6", "#f59e0b")).toEqual(
      deriveThemeVars("#3b82f6", "#f59e0b", "light"),
    );
  });
});

/**
 * These land as inline properties on the root element, which outrank the
 * stylesheet's own dark palette — so a dark scheme that only existed in CSS
 * would be overruled the moment a town set any colour of its own. That is why
 * the adaptation lives here rather than in a prefers-color-scheme block.
 */
describe("deriveThemeVars in the dark scheme", () => {
  /** Pulls the lightness back out of an `hsl(h, s%, l%)` string. */
  function lightnessOf(value: string): number {
    const match = /hsl\(\s*[\d.]+\s*,\s*[\d.]+%\s*,\s*([\d.]+)%\s*\)/.exec(value);
    expect(match, `not an hsl() value: ${value}`).not.toBeNull();
    return Number(match![1]);
  }

  function saturationOf(value: string): number {
    const match = /hsl\(\s*[\d.]+\s*,\s*([\d.]+)%/.exec(value);
    return Number(match![1]);
  }

  function hueOf(value: string): number {
    const match = /hsl\(\s*([\d.]+)/.exec(value);
    return Number(match![1]);
  }

  // A brand navy at 20% lightness is simply not visible on an ink background,
  // so the town's exact hex cannot be used as-is the way it is in light mode.
  it("lifts a dark brand colour into a band that reads on ink", () => {
    const vars = deriveThemeVars("#0a2e6b", undefined, "dark");
    expect(lightnessOf(vars["--color-primary"])).toBeGreaterThanOrEqual(64);
  });

  it("pulls a near-white brand colour down so it does not glare", () => {
    const vars = deriveThemeVars("#eef6ff", undefined, "dark");
    expect(lightnessOf(vars["--color-primary"])).toBeLessThanOrEqual(78);
  });

  // The whole point of a town's colour is that it is still the town's colour.
  it("keeps the town's hue while adapting its lightness", () => {
    const vars = deriveThemeVars("#0a2e6b", undefined, "dark");
    expect(hueOf(vars["--color-primary"])).toBe(hexToHSL("#0a2e6b")!.h);
  });

  // A colour already in the band comes through with its lightness untouched.
  it("leaves a colour that is already readable where it is", () => {
    const hsl = hexToHSL("#7fb2ff")!;
    expect(hsl.l).toBeGreaterThanOrEqual(64);
    expect(hsl.l).toBeLessThanOrEqual(78);
    expect(lightnessOf(deriveThemeVars("#7fb2ff", undefined, "dark")["--color-primary"]))
      .toBe(hsl.l);
  });

  // Hovering brightens on a dark background. Darkening, as the light scheme
  // does, would make the button recede exactly when it should respond.
  it("brightens the hover shade instead of darkening it", () => {
    const vars = deriveThemeVars("#0a7aff", undefined, "dark");
    expect(lightnessOf(vars["--color-primary-hover"]))
      .toBeGreaterThan(lightnessOf(vars["--color-primary"]));
  });

  // These are backgrounds behind text. At the light scheme's 94% they would be
  // near-white slabs in the middle of a dark page.
  it("makes the tinted backgrounds dark rather than pale", () => {
    const vars = deriveThemeVars("#0a7aff", undefined, "dark");
    expect(lightnessOf(vars["--color-primary-light"])).toBeLessThan(25);
    expect(lightnessOf(vars["--color-primary-subtle"]))
      .toBeLessThan(lightnessOf(vars["--color-primary-light"]));
  });

  // A fully saturated hue behind body text vibrates; the tint is a surface,
  // not a statement.
  it("desaturates the tints so a vivid brand hue does not glow behind text", () => {
    const vars = deriveThemeVars("#0a7aff", undefined, "dark");
    expect(hexToHSL("#0a7aff")!.s).toBe(100);
    expect(saturationOf(vars["--color-primary-light"])).toBeLessThanOrEqual(50);
  });

  it("adapts the accent the same way it adapts the primary", () => {
    const vars = deriveThemeVars(undefined, "#6b4e00", "dark");
    expect(lightnessOf(vars["--color-accent"])).toBeGreaterThanOrEqual(64);
    expect(lightnessOf(vars["--color-accent-hover"]))
      .toBeGreaterThan(lightnessOf(vars["--color-accent"]));
    expect(lightnessOf(vars["--color-accent-light"])).toBeLessThan(25);
  });

  it("emits the same set of properties as the light scheme", () => {
    expect(Object.keys(deriveThemeVars("#3b82f6", "#f59e0b", "dark")).sort()).toEqual(
      Object.keys(deriveThemeVars("#3b82f6", "#f59e0b", "light")).sort(),
    );
  });

  // The bad-colour rules do not change with the scheme.
  it("still contributes nothing for an unparseable colour", () => {
    expect(deriveThemeVars("nope", "also-nope", "dark")).toEqual({});
  });

  // Clamping is what makes the dark scheme's near-black --color-text-inverse
  // safe: whatever a town picks, the filled button under the label is light.
  it.each(["#000000", "#0a2e6b", "#3b82f6", "#ffffff", "#f59e0b"])(
    "keeps %s light enough to carry dark button text",
    (hex) => {
      expect(lightnessOf(deriveThemeVars(hex, undefined, "dark")["--color-primary"]))
        .toBeGreaterThanOrEqual(64);
    },
  );
});
