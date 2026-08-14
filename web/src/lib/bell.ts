/**
 * The shape of a struck bell, as numbers a WebAudio graph can be built from.
 *
 * Kept apart from the hook that plays it so the sound can be reasoned about and
 * tested without an AudioContext — jsdom has none, and "does this sound like a
 * bell" is a question about these ratios rather than about the browser API.
 */

/** One sine partial in the additive stack that makes up a strike. */
export interface Partial {
  /** Multiple of the strike note. */
  ratio: number;
  /** Peak amplitude before the master level, relative to the other partials. */
  gain: number;
  /** Seconds from the peak to silence. */
  decay: number;
}

/**
 * A cast bell's partials, in the proportions that make one sound cast rather
 * than synthesised.
 *
 * Two things do the work. The partials are inharmonic — 1.19 and 2.68 are not
 * multiples of anything — which is why a bell has a pitch but no single
 * frequency you can hum. And the third partial is a *minor* third above the
 * prime, the interval founders have tuned into tower bells for centuries and
 * the reason a bell sounds solemn rather than cheerful. A single sine wave has
 * neither property, which is why the 800Hz one read as a microwave.
 *
 * The upper partials decay fastest, so the strike is bright and what rings on
 * is the hum. That ordering is the whole envelope of a real bell.
 */
export const BELL_PARTIALS: readonly Partial[] = [
  { ratio: 0.5, gain: 0.30, decay: 1.05 },  // hum, an octave below the note
  { ratio: 1.0, gain: 0.42, decay: 0.90 },  // prime — the note you name it by
  { ratio: 1.19, gain: 0.34, decay: 0.70 }, // tierce, the minor third
  { ratio: 1.51, gain: 0.20, decay: 0.52 }, // quint
  { ratio: 2.02, gain: 0.24, decay: 0.44 }, // nominal
  { ratio: 2.68, gain: 0.10, decay: 0.22 },
  { ratio: 3.47, gain: 0.07, decay: 0.13 }, // the clapper's edge on the metal
  { ratio: 4.63, gain: 0.05, decay: 0.08 },
];

/**
 * The interval between the prime and the tierce, as a frequency ratio.
 *
 * A just minor third is 6/5. Bells run a little flat of it, which is part of
 * the character; this is the tolerance that still counts as one.
 */
export const MINOR_THIRD_RATIO = 1.2;

/**
 * How loud a strike is at its very peak, with every partial summed.
 *
 * Deliberately low. This plays unprompted when a neighbour posts, so it has to
 * be a sound somebody can leave switched on all day.
 */
export const MASTER_LEVEL = 0.09;

/** The bell rung when a post arrives: a low, warm G. */
export const BELL_NOTE_HZ = 392;

/** The lighter, shorter note for a reaction — the same bell, struck small. */
export const CHIME_NOTE_HZ = 659;
export const CHIME_DECAY_SCALE = 0.4;
export const CHIME_LEVEL_SCALE = 0.55;

/** Seconds of fade-in. Without it the strike starts with a click. */
export const ATTACK_SECONDS = 0.004;

/** One sine oscillator to schedule: everything the caller needs, resolved. */
export interface Voice {
  frequency: number;
  /** Absolute peak gain for this partial, master level already applied. */
  peak: number;
  /** Seconds from peak to silence. */
  decay: number;
}

export interface StrikeOptions {
  /** Shortens or lengthens every partial's tail together. */
  decayScale?: number;
  /** Scales the whole strike's loudness. */
  levelScale?: number;
}

/**
 * bellVoices resolves the partial table against a note into the oscillators a
 * single strike needs.
 *
 * Scaling decay rather than giving the chime its own table keeps the two sounds
 * recognisably the same bell — a reaction and a post should not sound like they
 * came from different towns.
 */
export function bellVoices(frequency: number, options: StrikeOptions = {}): Voice[] {
  const { decayScale = 1, levelScale = 1 } = options;

  return BELL_PARTIALS.map((partial) => ({
    frequency: frequency * partial.ratio,
    peak: partial.gain * MASTER_LEVEL * levelScale,
    decay: partial.decay * decayScale,
  }));
}

/** How long a strike takes to fall silent, for scheduling the oscillator stops. */
export function strikeDuration(voices: readonly Voice[]): number {
  return voices.reduce((longest, voice) => Math.max(longest, voice.decay), 0);
}
