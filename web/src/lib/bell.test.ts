import { describe, expect, it } from "vitest";
import {
  BELL_NOTE_HZ,
  BELL_PARTIALS,
  CHIME_DECAY_SCALE,
  CHIME_LEVEL_SCALE,
  CHIME_NOTE_HZ,
  MASTER_LEVEL,
  MINOR_THIRD_RATIO,
  bellVoices,
  strikeDuration,
} from "./bell";

/**
 * These pin the things that make the sound a bell rather than a beep. The old
 * one was a single 800Hz sine, which is a microwave: one frequency, harmonic
 * with nothing, decaying uniformly.
 */

describe("BELL_PARTIALS", () => {
  it("stacks several partials rather than sounding one frequency", () => {
    expect(BELL_PARTIALS.length).toBeGreaterThan(4);
  });

  // The interval a bell founder tunes in, and the reason a bell reads as solemn.
  it("carries a minor third above the prime", () => {
    const tierce = BELL_PARTIALS.find(
      (p) => Math.abs(p.ratio - MINOR_THIRD_RATIO) < 0.02,
    );
    expect(tierce).toBeDefined();
  });

  // A stack of whole-number multiples is an organ pipe, not a bell.
  it("is inharmonic — some partials are not multiples of the prime", () => {
    const inharmonic = BELL_PARTIALS.filter(
      (p) => Math.abs(p.ratio - Math.round(p.ratio)) > 0.1,
    );
    expect(inharmonic.length).toBeGreaterThan(1);
  });

  it("rings below the prime as well as above it, which is the hum", () => {
    expect(BELL_PARTIALS.some((p) => p.ratio < 1)).toBe(true);
  });

  // The bright edge of the strike has to die away first, leaving the hum. Get
  // this backwards and it sounds like a synth pad swelling.
  it("lets the high partials die away before the low ones", () => {
    const lowest = BELL_PARTIALS.reduce((a, b) => (a.ratio < b.ratio ? a : b));
    const highest = BELL_PARTIALS.reduce((a, b) => (a.ratio > b.ratio ? a : b));
    expect(highest.decay).toBeLessThan(lowest.decay);
  });
});

describe("bellVoices", () => {
  it("resolves each partial against the note", () => {
    const voices = bellVoices(400);
    expect(voices).toHaveLength(BELL_PARTIALS.length);
    expect(voices[0].frequency).toBeCloseTo(400 * BELL_PARTIALS[0].ratio);
  });

  // This plays unprompted whenever a neighbour posts. Somebody has to be able
  // to leave it switched on all day.
  it("stays quiet even with every partial peaking at once", () => {
    const peak = bellVoices(BELL_NOTE_HZ).reduce((sum, v) => sum + v.peak, 0);
    expect(peak).toBeGreaterThan(0);
    expect(peak).toBeLessThan(0.2);
  });

  it("scales every partial's tail together", () => {
    const plain = bellVoices(400);
    const short = bellVoices(400, { decayScale: 0.5 });
    short.forEach((voice, i) => expect(voice.decay).toBeCloseTo(plain[i].decay * 0.5));
  });

  it("scales loudness without touching pitch", () => {
    const plain = bellVoices(400);
    const quiet = bellVoices(400, { levelScale: 0.5 });
    quiet.forEach((voice, i) => {
      expect(voice.peak).toBeCloseTo(plain[i].peak * 0.5);
      expect(voice.frequency).toBe(plain[i].frequency);
    });
  });

  it("applies the master level to the relative partial gains", () => {
    expect(bellVoices(400)[0].peak).toBeCloseTo(BELL_PARTIALS[0].gain * MASTER_LEVEL);
  });
});

describe("the two strikes", () => {
  // A reaction and a post should sound like the same bell struck differently,
  // not like two different towns.
  it("chimes higher, shorter and quieter than it rings", () => {
    const bell = bellVoices(BELL_NOTE_HZ);
    const chime = bellVoices(CHIME_NOTE_HZ, {
      decayScale: CHIME_DECAY_SCALE,
      levelScale: CHIME_LEVEL_SCALE,
    });

    expect(chime[0].frequency).toBeGreaterThan(bell[0].frequency);
    expect(strikeDuration(chime)).toBeLessThan(strikeDuration(bell));
    expect(chime[0].peak).toBeLessThan(bell[0].peak);
  });

  it("keeps both strikes short enough not to overlap the next arrival", () => {
    expect(strikeDuration(bellVoices(BELL_NOTE_HZ))).toBeLessThan(2);
  });
});

describe("strikeDuration", () => {
  it("reports the longest tail, which is when the last oscillator can stop", () => {
    expect(strikeDuration([
      { frequency: 1, peak: 1, decay: 0.2 },
      { frequency: 2, peak: 1, decay: 0.9 },
    ])).toBe(0.9);
  });

  it("reports nothing for no voices rather than -Infinity", () => {
    expect(strikeDuration([])).toBe(0);
  });
});
