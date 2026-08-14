interface MuteToggleProps {
  muted: boolean;
  /**
   * How many times the bell has rung this session. Used only as a key, so each
   * arrival remounts the emoji and replays the one-shot CSS animation — there is
   * no timer to reset.
   */
  arrivals: number;
  onToggle: () => void;
}

/**
 * The bell-sound switch.
 *
 * It is a component rather than markup inside the feed because it has to exist
 * at every breakpoint: the sounds it silences are played by the feed page
 * itself, with no regard for viewport width, so a toggle rendered only inside a
 * `lg:hidden` block left desktop readers with audio they could not turn off.
 *
 * The state lives with the feed, which is what reads it before ringing. This
 * only draws it.
 */
export default function MuteToggle({ muted, arrivals, onToggle }: MuteToggleProps) {
  const label = muted ? "Unmute bell sounds" : "Mute bell sounds";

  return (
    <button
      type="button"
      onClick={onToggle}
      className="tint-on-hover flex h-10 w-10 items-center justify-center rounded-lg"
      style={{ color: "var(--color-text-secondary)" }}
      aria-pressed={muted}
      aria-label={label}
      title={label}
    >
      <span
        key={arrivals}
        aria-hidden="true"
        className={`text-xl ${arrivals > 0 ? "animate-ring inline-block" : ""}`}
      >
        {muted ? "🔕" : "🔔"}
      </span>
    </button>
  );
}
