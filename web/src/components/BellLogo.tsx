interface BellLogoProps {
  size?: number;
  animate?: boolean;
  className?: string;
}

export default function BellLogo({ size = 32, animate = false, className = "" }: BellLogoProps) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.5"
      strokeLinecap="round"
      strokeLinejoin="round"
      className={`${animate ? "animate-bell-ring" : ""} ${className}`}
      style={{ color: "var(--color-primary)" }}
    >
      <path d="M18 8A6 6 0 0 0 6 8c0 7-3 9-3 9h18s-3-2-3-9" />
      <path d="M13.73 21a2 2 0 0 1-3.46 0" />
    </svg>
  );
}
