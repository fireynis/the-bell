export interface HSL {
  /** Degrees, 0-360. */
  h: number;
  /** Percent, 0-100. */
  s: number;
  /** Percent, 0-100. */
  l: number;
}

/** Matches #rgb and #rrggbb, with or without the leading hash, either case. */
const HEX_PATTERN = /^#?([a-f\d]{3}|[a-f\d]{6})$/i;

/**
 * hexToHSL converts a hex colour to HSL so shades can be derived from it.
 *
 * Returns null rather than throwing for anything unparseable: these colours
 * come from town config that an admin types by hand, and a typo must leave the
 * stylesheet defaults in place rather than blanking the app.
 */
export function hexToHSL(hex: string): HSL | null {
  if (typeof hex !== "string") return null;

  const match = HEX_PATTERN.exec(hex.trim());
  if (!match) return null;

  // #abc is the CSS shorthand for #aabbcc; expanding it here means an admin
  // pasting a shorthand colour gets a theme rather than silence.
  const digits =
    match[1].length === 3
      ? match[1].replace(/./g, (d) => d + d)
      : match[1];

  const r = parseInt(digits.slice(0, 2), 16) / 255;
  const g = parseInt(digits.slice(2, 4), 16) / 255;
  const b = parseInt(digits.slice(4, 6), 16) / 255;

  const max = Math.max(r, g, b);
  const min = Math.min(r, g, b);
  const l = (max + min) / 2;

  let h = 0;
  let s = 0;

  // max === min is a grey, which has no meaningful hue; leaving both at zero
  // avoids dividing by a zero chroma.
  if (max !== min) {
    const d = max - min;
    s = l > 0.5 ? d / (2 - max - min) : d / (max + min);
    switch (max) {
      case r:
        h = ((g - b) / d + (g < b ? 6 : 0)) / 6;
        break;
      case g:
        h = ((b - r) / d + 2) / 6;
        break;
      default:
        h = ((r - g) / d + 4) / 6;
        break;
    }
  }

  return { h: Math.round(h * 360), s: Math.round(s * 100), l: Math.round(l * 100) };
}

/** Which palette the derived shades have to sit in. */
export type ColorScheme = "light" | "dark";

/** How much darker the hover shade of each colour is, in lightness points. */
const PRIMARY_HOVER_DARKEN = 10;
const ACCENT_HOVER_DARKEN = 8;

/** Lightness of the tinted background shades, which stay pale at any hue. */
const LIGHT_SHADE_LIGHTNESS = 94;
const SUBTLE_SHADE_LIGHTNESS = 97;

/**
 * The band a town's colour is pulled into on a dark background.
 *
 * A brand navy at 20% lightness is invisible on ink and a pastel at 90% glares,
 * so both ends are pulled in. The floor is what makes the dark scheme's
 * --color-text-inverse safe to set to near-black: whatever a town picks, the
 * filled button underneath the label is light enough to carry dark text.
 */
const DARK_LIGHTNESS_FLOOR = 64;
const DARK_LIGHTNESS_CEILING = 78;

/** On a dark background the hover shade brightens instead of darkening. */
const DARK_HOVER_LIGHTEN = 8;

/**
 * The tinted backgrounds in the dark scheme: dark enough to read as a surface,
 * and desaturated so a vivid brand hue does not glow behind body text.
 */
const DARK_TINT_LIGHTNESS = 18;
const DARK_TINT_MAX_SATURATION = 50;
const DARK_SUBTLE_LIGHTNESS = 14;
const DARK_SUBTLE_MAX_SATURATION = 45;

function clampLightness(value: number): number {
  return Math.max(DARK_LIGHTNESS_FLOOR, Math.min(DARK_LIGHTNESS_CEILING, value));
}

function hsl(h: number, s: number, l: number): string {
  return `hsl(${h}, ${clampPercent(s)}%, ${clampPercent(l)}%)`;
}

/**
 * Keeps a derived percentage inside the range CSS accepts. Every value reaching
 * this comes from hexToHSL, which only ever returns finite 0-100 numbers, so
 * offsetting one can push it past a bound but never make it NaN.
 */
function clampPercent(value: number): number {
  return Math.max(0, Math.min(100, value));
}

/**
 * deriveThemeVars builds the CSS custom properties for a town's colours.
 *
 * Each colour yields a hover shade and one or two tints rather than requiring
 * an admin to pick four colours that agree with each other. A colour that
 * cannot be parsed contributes nothing, so a bad accent still leaves a good
 * primary applied.
 *
 * The scheme has to be decided here rather than in a `prefers-color-scheme`
 * block, because these land as inline custom properties on the root element and
 * an inline property outranks every stylesheet rule. A dark scheme written in
 * CSS alone would be overruled by the town's own colours the moment it had any.
 *
 * In the light scheme the primary is passed through as the exact hex the town
 * chose. In the dark scheme it is pulled into a readable band first — a brand
 * colour nobody can see is not the brand colour either.
 */
export function deriveThemeVars(
  primary?: string,
  accent?: string,
  scheme: ColorScheme = "light",
): Record<string, string> {
  const vars: Record<string, string> = {};
  const dark = scheme === "dark";

  const primaryHSL = primary ? hexToHSL(primary) : null;
  if (primary && primaryHSL) {
    const base = dark ? clampLightness(primaryHSL.l) : primaryHSL.l;

    vars["--color-primary"] = dark ? hsl(primaryHSL.h, primaryHSL.s, base) : primary;
    vars["--color-primary-hover"] = hsl(
      primaryHSL.h,
      primaryHSL.s,
      dark ? base + DARK_HOVER_LIGHTEN : base - PRIMARY_HOVER_DARKEN,
    );
    vars["--color-primary-light"] = dark
      ? hsl(primaryHSL.h, Math.min(primaryHSL.s, DARK_TINT_MAX_SATURATION), DARK_TINT_LIGHTNESS)
      : hsl(primaryHSL.h, primaryHSL.s, LIGHT_SHADE_LIGHTNESS);
    vars["--color-primary-subtle"] = dark
      ? hsl(primaryHSL.h, Math.min(primaryHSL.s, DARK_SUBTLE_MAX_SATURATION), DARK_SUBTLE_LIGHTNESS)
      : hsl(primaryHSL.h, primaryHSL.s, SUBTLE_SHADE_LIGHTNESS);
  }

  const accentHSL = accent ? hexToHSL(accent) : null;
  if (accent && accentHSL) {
    const base = dark ? clampLightness(accentHSL.l) : accentHSL.l;

    vars["--color-accent"] = dark ? hsl(accentHSL.h, accentHSL.s, base) : accent;
    vars["--color-accent-hover"] = hsl(
      accentHSL.h,
      accentHSL.s,
      dark ? base + DARK_HOVER_LIGHTEN : base - ACCENT_HOVER_DARKEN,
    );
    vars["--color-accent-light"] = dark
      ? hsl(accentHSL.h, Math.min(accentHSL.s, DARK_TINT_MAX_SATURATION), DARK_TINT_LIGHTNESS)
      : hsl(accentHSL.h, accentHSL.s, LIGHT_SHADE_LIGHTNESS);
  }

  return vars;
}
