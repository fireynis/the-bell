interface LockGlyphProps {
  size?: number;
}

/**
 * The padlock that marks a control someone can see but cannot use yet.
 *
 * It is decorative in the strict sense — every place it appears also states the
 * same thing in words, either in the control's accessible name or in the page
 * behind it — so it is hidden from assistive technology rather than given a
 * label that would be read out twice.
 */
export default function LockGlyph({ size = 14 }: LockGlyphProps) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      focusable="false"
    >
      <rect x="4" y="11" width="16" height="10" rx="2" />
      <path d="M8 11V7a4 4 0 0 1 8 0v4" />
    </svg>
  );
}
