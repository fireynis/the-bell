import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

/**
 * Guards the colour tokens against going unreadable.
 *
 * This is a tool a town is obliged to make usable by everybody, and the palette
 * is now two palettes — a change that looks right in one scheme can quietly
 * fail in the other. So rather than trusting an eye on a good monitor, the
 * stylesheet is parsed and every foreground is checked against the background
 * it is actually drawn on.
 *
 * The town's own primary and accent are deliberately absent: those arrive at
 * runtime from town config, and deriveThemeVars is what keeps them legible.
 * See color.test.ts for that half.
 */

/**
 * Read off disk rather than imported: Vitest stubs CSS imports to an empty
 * string, and an empty stylesheet would make every assertion below vacuous.
 */
const CSS = readFileSync(resolve(process.cwd(), "src/index.css"), "utf8");

/** The `:root` block, up to where the dark scheme starts overriding it. */
function lightTokens(): Record<string, string> {
  return parseTokens(CSS.slice(CSS.indexOf(":root {"), darkStart()));
}

/** The dark scheme is a set of overrides, so it inherits anything it omits. */
function darkTokens(): Record<string, string> {
  return { ...lightTokens(), ...parseTokens(CSS.slice(darkStart(), CSS.indexOf("@theme {"))) };
}

function darkStart(): number {
  const at = CSS.indexOf("@media (prefers-color-scheme: dark)");
  expect(at, "no dark scheme in index.css").toBeGreaterThan(-1);
  return at;
}

function parseTokens(block: string): Record<string, string> {
  const out: Record<string, string> = {};
  for (const [, name, hex] of block.matchAll(/(--[\w-]+):\s*(#[0-9A-Fa-f]{6})/g)) {
    out[name] = hex;
  }
  return out;
}

/** WCAG 2.1 relative luminance. */
function luminance(hex: string): number {
  const channels = [1, 3, 5].map((i) => {
    const c = parseInt(hex.slice(i, i + 2), 16) / 255;
    return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
  });
  return 0.2126 * channels[0] + 0.7152 * channels[1] + 0.0722 * channels[2];
}

function contrast(foreground: string, background: string): number {
  const a = luminance(foreground);
  const b = luminance(background);
  return (Math.max(a, b) + 0.05) / (Math.min(a, b) + 0.05);
}

/** Every foreground token, named with the surface it is actually drawn on. */
const PAIRS: ReadonlyArray<[label: string, foreground: string, background: string]> = [
  ["body text on a card", "--color-text", "--color-surface"],
  ["body text on the page", "--color-text", "--color-surface-secondary"],
  ["secondary text on a card", "--color-text-secondary", "--color-surface"],
  ["secondary text on the page", "--color-text-secondary", "--color-surface-secondary"],
  ["metadata on a card", "--color-text-tertiary", "--color-surface"],
  ["metadata on the page", "--color-text-tertiary", "--color-surface-secondary"],
  ["a quiet button's label", "--color-text-secondary", "--color-surface-tertiary"],
  ["a success notice", "--color-success", "--color-success-light"],
  ["a warning notice", "--color-warning", "--color-warning-light"],
  ["an error notice", "--color-danger", "--color-danger-light"],
  ["an info notice", "--color-info", "--color-info-light"],
  ["a danger button's label", "--color-text-inverse", "--color-danger"],
  ["a danger button hovered", "--color-text-inverse", "--color-danger-hover"],
  ["the council badge", "--color-role-council", "--color-role-council-bg"],
  ["the moderator badge", "--color-role-moderator", "--color-role-moderator-bg"],
  ["the member badge", "--color-role-member", "--color-role-member-bg"],
  ["the pending badge", "--color-role-pending", "--color-role-pending-bg"],
  ["the banned badge", "--color-role-banned", "--color-role-banned-bg"],
];

/** WCAG AA for text at the sizes these tokens are used at. */
const AA_NORMAL_TEXT = 4.5;

describe.each([
  ["light", lightTokens],
  ["dark", darkTokens],
])("%s scheme contrast", (_scheme, tokens) => {
  it.each(PAIRS)("%s clears AA", (_label, foreground, background) => {
    const palette = tokens();
    expect(palette[foreground], `${foreground} is not defined`).toBeDefined();
    expect(palette[background], `${background} is not defined`).toBeDefined();

    expect(contrast(palette[foreground], palette[background]))
      .toBeGreaterThanOrEqual(AA_NORMAL_TEXT);
  });
});

describe("the two schemes", () => {
  // Everything else here parses this text, so an empty read has to fail loudly
  // rather than turn every assertion below into a no-op.
  it("actually found the stylesheet", () => {
    expect(CSS.length).toBeGreaterThan(1000);
    expect(Object.keys(lightTokens()).length).toBeGreaterThan(20);
  });

  // A token the dark scheme forgets to override keeps its light value, which
  // is how you end up with near-black text on a near-black card.
  it("overrides every colour that carries meaning in the dark", () => {
    const dark = parseTokens(CSS.slice(darkStart(), CSS.indexOf("@theme {")));
    for (const name of [
      "--color-surface",
      "--color-surface-secondary",
      "--color-text",
      "--color-text-inverse",
      "--color-border",
      "--color-danger",
      "--color-success",
      "--color-warning",
    ]) {
      expect(dark[name], `${name} is not re-stated for the dark scheme`).toBeDefined();
    }
  });

  // The page sits behind cards that have to lift off it.
  it.each([
    ["light", lightTokens],
    ["dark", darkTokens],
  ])("keeps %s cards distinguishable from the page behind them", (_scheme, tokens) => {
    const palette = tokens();
    expect(palette["--color-surface"]).not.toBe(palette["--color-surface-secondary"]);
  });

  // color-scheme is what tells the browser to restyle scrollbars and form
  // controls; without it the native widgets stay light on a dark page.
  it("declares color-scheme for both", () => {
    expect(CSS).toMatch(/color-scheme:\s*light/);
    expect(CSS.slice(darkStart())).toMatch(/color-scheme:\s*dark/);
  });
});
