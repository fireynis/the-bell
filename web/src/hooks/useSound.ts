import { useCallback, useRef } from "react";
import {
  ATTACK_SECONDS,
  BELL_NOTE_HZ,
  CHIME_DECAY_SCALE,
  CHIME_LEVEL_SCALE,
  CHIME_NOTE_HZ,
  bellVoices,
  strikeDuration,
  type StrikeOptions,
  type Voice,
} from "../lib/bell.ts";

/** Never quite zero: exponentialRampToValueAtTime cannot ramp to silence. */
const SILENCE = 0.0001;

/**
 * useSound rings the town bell.
 *
 * The sound is synthesised rather than loaded, so the app ships no audio files
 * and nothing has to be fetched before the first post of a session can announce
 * itself. What it synthesises is a stack of inharmonic partials with separate
 * decays — see lib/bell.ts for why that is what makes it a bell.
 */
export function useSound() {
  const ctxRef = useRef<AudioContext | null>(null);

  const strike = useCallback((frequency: number, options?: StrikeOptions) => {
    try {
      if (!ctxRef.current) {
        ctxRef.current = new AudioContext();
      }
      const ctx = ctxRef.current;
      if (ctx.state === "suspended") {
        ctx.resume();
      }

      const now = ctx.currentTime;
      const voices = bellVoices(frequency, options);

      for (const voice of voices) {
        playVoice(ctx, voice, now);
      }

      return strikeDuration(voices);
    } catch {
      // Silently skip if AudioContext fails (autoplay policy, etc.)
      return 0;
    }
  }, []);

  const playBell = useCallback(() => {
    strike(BELL_NOTE_HZ);
  }, [strike]);

  const playChime = useCallback(() => {
    strike(CHIME_NOTE_HZ, {
      decayScale: CHIME_DECAY_SCALE,
      levelScale: CHIME_LEVEL_SCALE,
    });
  }, [strike]);

  return { playBell, playChime };
}

/**
 * Schedules one partial: a short fade in so the strike does not click, then an
 * exponential fall, which is how a struck metal body actually loses energy.
 */
function playVoice(ctx: AudioContext, voice: Voice, startedAt: number) {
  const osc = ctx.createOscillator();
  const gain = ctx.createGain();

  osc.connect(gain);
  gain.connect(ctx.destination);

  osc.type = "sine";
  osc.frequency.value = voice.frequency;

  const peakAt = startedAt + ATTACK_SECONDS;
  const endsAt = peakAt + voice.decay;

  gain.gain.setValueAtTime(0, startedAt);
  gain.gain.linearRampToValueAtTime(voice.peak, peakAt);
  gain.gain.exponentialRampToValueAtTime(SILENCE, endsAt);

  osc.start(startedAt);
  osc.stop(endsAt);
}
