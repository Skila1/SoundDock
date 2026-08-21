import { streamUrl } from "@/lib/api";
import { loadDevicePrefs } from "@/lib/device";
import { offlineObjectUrl } from "@/offline";

export type TrackGainFields = {
  replaygain_track_gain?: number | null;
  replaygain_album_gain?: number | null;
  replaygain_track_peak?: number | null;
  replaygain_album_peak?: number | null;
  manual_gain_db?: number | null;
  encoder_delay?: number | null;
  encoder_padding?: number | null;
  sample_rate?: number | null;
};

type SlotGraph = {
  source: MediaElementAudioSourceNode;
  gain: GainNode;
};

type SharedGraph = {
  ctx: AudioContext;
  mix: GainNode;
  analyser: AnalyserNode | null;
};

type SinkEl = HTMLAudioElement & { setSinkId?: (id: string) => Promise<void> };
type SinkCtx = AudioContext & { setSinkId?: (id: string) => Promise<void> };

const graphs = new WeakMap<HTMLAudioElement, SlotGraph>();
let shared: SharedGraph | null | undefined;
let slots: HTMLAudioElement[] = [];
let active = 0;
let userVolume = 1;
let userMuted = false;
let rgMultiplier = 1;
let fadeA = 1;
let fadeB = 1;
let playbackRate = 1;

function makeElement() {
  const a = new Audio();
  a.preload = "auto";
  a.crossOrigin = "anonymous";
  return a;
}

function both(): HTMLAudioElement[] {
  if (!slots.length) {
    slots = [makeElement()];
    try {
      slots.push(makeElement());
    } catch {
      /* single-element fallback */
    }
  }
  return slots;
}

export function getAudio() {
  return both()[active] || both()[0];
}

export function getIdleAudio() {
  const all = both();
  if (all.length < 2) return null;
  return all[active === 0 ? 1 : 0];
}

function getShared(): SharedGraph | null {
  if (shared !== undefined) return shared;
  try {
    const Ctx = window.AudioContext || (window as unknown as { webkitAudioContext?: typeof AudioContext }).webkitAudioContext;
    if (!Ctx) {
      shared = null;
      return null;
    }
    const ctx = new Ctx();
    const mix = ctx.createGain();
    mix.gain.value = 1;
    let analyser: AnalyserNode | null = null;
    try {
      analyser = ctx.createAnalyser();
      analyser.fftSize = 256;
      mix.connect(analyser);
      analyser.connect(ctx.destination);
    } catch {
      try {
        mix.connect(ctx.destination);
      } catch {
        shared = null;
        return null;
      }
    }
    shared = { ctx, mix, analyser };
    return shared;
  } catch {
    shared = null;
    return null;
  }
}

/** Non-fatal. If this throws, the HTMLAudioElement must keep playing. */
export function ensureGraph(el: HTMLAudioElement): SlotGraph | null {
  const hit = graphs.get(el);
  if (hit) return hit;
  const sh = getShared();
  if (!sh) return null;
  try {
    const source = sh.ctx.createMediaElementSource(el);
    let gain: GainNode;
    try {
      gain = sh.ctx.createGain();
      gain.gain.value = 1;
      source.connect(gain);
      gain.connect(sh.mix);
    } catch {
      try {
        source.connect(sh.mix);
      } catch {
        try {
          source.connect(sh.ctx.destination);
        } catch {
          /* native element output only */
        }
      }
      gain = sh.ctx.createGain();
      gain.gain.value = 1;
    }
    const g: SlotGraph = { source, gain };
    graphs.set(el, g);
    return g;
  } catch {
    return null;
  }
}

export function getAnalyser() {
  return getShared()?.analyser ?? null;
}

export async function resumeGraph() {
  const sh = getShared();
  if (!sh) return;
  try {
    if (sh.ctx.state === "suspended") await sh.ctx.resume();
  } catch {
    /* autoplay / policy */
  }
}

export function replayGainMultiplier(mode: string | undefined, t?: TrackGainFields | null) {
  if (!t || !mode || mode === "off") {
    const manual = t?.manual_gain_db;
    if (manual == null || Number.isNaN(manual)) return 1;
    return Math.pow(10, manual / 20);
  }
  const db =
    (mode === "album" ? t.replaygain_album_gain : t.replaygain_track_gain) ??
    t.replaygain_track_gain ??
    t.replaygain_album_gain ??
    0;
  const peak =
    (mode === "album" ? t.replaygain_album_peak : t.replaygain_track_peak) ??
    t.replaygain_track_peak ??
    t.replaygain_album_peak ??
    1;
  const manual = t.manual_gain_db ?? 0;
  let mult = Math.pow(10, (Number(db) + Number(manual)) / 20);
  const p = Number(peak);
  if (p > 0 && mult * p > 1) mult = 1 / p;
  if (!Number.isFinite(mult) || mult <= 0) return 1;
  return Math.min(4, mult);
}

export function looksGapless(cur?: TrackGainFields | null, next?: TrackGainFields | null) {
  if (!cur || !next) return false;
  const has = (t: TrackGainFields) => t.encoder_delay != null || t.encoder_padding != null;
  return has(cur) && has(next);
}

export function encoderStartSeconds(t?: TrackGainFields | null) {
  if (!t || t.encoder_delay == null) return 0;
  const sr = t.sample_rate && t.sample_rate > 0 ? t.sample_rate : 44100;
  return Math.max(0, t.encoder_delay / sr);
}

export function encoderEndPadSeconds(t?: TrackGainFields | null) {
  if (!t || t.encoder_padding == null) return 0;
  const sr = t.sample_rate && t.sample_rate > 0 ? t.sample_rate : 44100;
  return Math.max(0, t.encoder_padding / sr);
}

function applySlotGain(index: number, fade: number) {
  const el = both()[index];
  if (!el) return;
  const g = graphs.get(el);
  const linear = (userMuted ? 0 : userVolume) * rgMultiplier * fade;
  if (g) {
    try {
      g.gain.gain.value = rgMultiplier * fade;
    } catch {
      /* keep element volume */
    }
    el.volume = userMuted ? 0 : userVolume;
  } else {
    el.volume = Math.min(1, Math.max(0, linear > 1 ? userMuted ? 0 : userVolume : linear));
  }
}

export function applyVolume(volume: number, muted: boolean) {
  userVolume = Math.min(1, Math.max(0, volume));
  userMuted = muted;
  applySlotGain(0, fadeA);
  applySlotGain(1, fadeB);
}

export function applyReplayGain(mult: number) {
  rgMultiplier = Number.isFinite(mult) && mult > 0 ? Math.min(4, mult) : 1;
  applySlotGain(0, fadeA);
  applySlotGain(1, fadeB);
}

export function applyRate(rate: number) {
  playbackRate = rate > 0 ? Math.min(2, Math.max(0.5, rate)) : 1;
  for (const el of both()) {
    try {
      el.playbackRate = playbackRate;
    } catch {
      /* ignore */
    }
  }
}

export async function applySink(sinkId: string) {
  const id = sinkId || "default";
  const sh = getShared();
  try {
    await (sh?.ctx as SinkCtx | undefined)?.setSinkId?.(id === "default" ? "" : id);
  } catch {
    /* non-fatal */
  }
  for (const el of both()) {
    try {
      await (el as SinkEl).setSinkId?.(id === "default" ? "" : id);
    } catch {
      /* non-fatal */
    }
  }
}

export async function listAudioOutputs() {
  try {
    const devices = await navigator.mediaDevices.enumerateDevices();
    return devices
      .filter((d) => d.kind === "audiooutput")
      .map((d, i) => ({ deviceId: d.deviceId, label: d.label || `Output ${i + 1}` }));
  } catch {
    return [] as { deviceId: string; label: string }[];
  }
}

export function setFade(slot: number, fade: number) {
  if (slot === 0) fadeA = fade;
  else fadeB = fade;
  applySlotGain(slot, fade);
}

export function rampFade(slot: number, from: number, to: number, ms: number) {
  const start = performance.now();
  const tick = (now: number) => {
    const t = ms <= 0 ? 1 : Math.min(1, (now - start) / ms);
    setFade(slot, from + (to - from) * t);
    if (t < 1) requestAnimationFrame(tick);
  };
  requestAnimationFrame(tick);
}

export function pauseAll() {
  for (const el of both()) {
    try {
      el.pause();
    } catch {
      /* ignore */
    }
  }
}

export function stopElement(el: HTMLAudioElement | null) {
  if (!el) return;
  try {
    el.pause();
    el.removeAttribute("src");
    el.load();
  } catch {
    /* ignore */
  }
}

export async function bindTrack(el: HTMLAudioElement, id: string) {
  let next = streamUrl(id);
  try {
    const blob = await offlineObjectUrl(id);
    if (blob) next = blob;
  } catch {
    /* Cache Storage unavailable */
  }
  if (el.dataset.trackId === id && el.src) return;
  el.dataset.trackId = id;
  el.src = next;
}

export function preloadTrack(id: string) {
  const idle = getIdleAudio();
  if (!idle || !id) return;
  void bindTrack(idle, id).then(() => {
    try {
      idle.load();
    } catch {
      /* ignore */
    }
  });
}

export function swapActive() {
  const all = both();
  if (all.length < 2) return getAudio();
  active = active === 0 ? 1 : 0;
  return getAudio();
}

export function activeIndex() {
  return active;
}

export async function playActive() {
  const el = getAudio();
  ensureGraph(el);
  await resumeGraph();
  applyRate(playbackRate);
  applyVolume(userVolume, userMuted);
  const sink = loadDevicePrefs().sinkId;
  if (sink) await applySink(sink);
  await el.play();
}

export function seekActive(ms: number) {
  const el = getAudio();
  try {
    el.currentTime = Math.max(0, ms / 1000);
  } catch {
    /* not ready */
  }
}

export function positionMs() {
  return (getAudio().currentTime || 0) * 1000;
}

export function durationMs() {
  const d = getAudio().duration;
  return Number.isFinite(d) ? d * 1000 : 0;
}

export function remainingMs() {
  const el = getAudio();
  const d = el.duration;
  if (!Number.isFinite(d)) return Infinity;
  return Math.max(0, (d - el.currentTime) * 1000);
}
