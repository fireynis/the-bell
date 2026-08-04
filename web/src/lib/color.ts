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

/** How much darker the hover shade of each colour is, in lightness points. */
const PRIMARY_HOVER_DARKEN = 10;
const ACCENT_HOVER_DARKEN = 8;

/** Lightness of the tinted background shades, which stay pale at any hue. */
const LIGHT_SHADE_LIGHTNESS = 94;
const SUBTLE_SHADE_LIGHTNESS = 97;

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
 * Each colour yields a hover shade and one or two pale tints rather than
 * requiring an admin to pick four colours that agree with each other. A colour
 * that cannot be parsed contributes nothing, so a bad accent still leaves a
 * good primary applied.
 */
export function deriveThemeVars(primary?: string, accent?: string): Record<string, string> {
  const vars: Record<string, string> = {};

  const primaryHSL = primary ? hexToHSL(primary) : null;
  if (primary && primaryHSL) {
    vars["--color-primary"] = primary;
    vars["--color-primary-hover"] = hsl(
      primaryHSL.h,
      primaryHSL.s,
      primaryHSL.l - PRIMARY_HOVER_DARKEN,
    );
    vars["--color-primary-light"] = hsl(primaryHSL.h, primaryHSL.s, LIGHT_SHADE_LIGHTNESS);
    vars["--color-primary-subtle"] = hsl(primaryHSL.h, primaryHSL.s, SUBTLE_SHADE_LIGHTNESS);
  }

  const accentHSL = accent ? hexToHSL(accent) : null;
  if (accent && accentHSL) {
    vars["--color-accent"] = accent;
    vars["--color-accent-hover"] = hsl(
      accentHSL.h,
      accentHSL.s,
      accentHSL.l - ACCENT_HOVER_DARKEN,
    );
    vars["--color-accent-light"] = hsl(accentHSL.h, accentHSL.s, LIGHT_SHADE_LIGHTNESS);
  }

  return vars;
}
