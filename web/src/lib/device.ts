const KEY = "sd-device";

export type OutputTarget = "browser" | "discord";

export type VoiceState = {
  discord_enabled: boolean;
  linked: boolean;
  in_voice: boolean;
  guild_id?: string | null;
  channel_id?: string | null;
};

export type DevicePrefs = {
  deviceId: string;
  /** Last explicit Browser/Discord click. Null means follow the default. */
  outputManual: OutputTarget | null;
  sinkId: string;
  autoplay: boolean;
  autoplaySet?: boolean;
  visualizer: boolean;
  playbackRate: number;
  tinyMode: boolean;
};

function newDeviceId() {
  try {
    return crypto.randomUUID();
  } catch {
    return `browser-${Date.now().toString(36)}`;
  }
}

const defaults = (): DevicePrefs => ({
  deviceId: newDeviceId(),
  outputManual: null,
  sinkId: "",
  autoplay: false,
  visualizer: false,
  playbackRate: 1,
  tinyMode: false
});

export function loadDevicePrefs(): DevicePrefs {
  const base = defaults();
  try {
    const raw = localStorage.getItem(KEY);
    if (!raw) {
      localStorage.setItem(KEY, JSON.stringify(base));
      return base;
    }
    const parsed = JSON.parse(raw) as Partial<DevicePrefs>;
    const next: DevicePrefs = {
      deviceId: parsed.deviceId || base.deviceId,
      outputManual: parsed.outputManual === "browser" || parsed.outputManual === "discord" ? parsed.outputManual : null,
      sinkId: typeof parsed.sinkId === "string" ? parsed.sinkId : "",
      autoplay: parsed.autoplaySet ? parsed.autoplay === true : false,
      visualizer: !!parsed.visualizer,
      playbackRate: typeof parsed.playbackRate === "number" && parsed.playbackRate > 0 ? parsed.playbackRate : 1,
      tinyMode: !!parsed.tinyMode
    };
    return next;
  } catch {
    return base;
  }
}

export function saveDevicePrefs(partial: Partial<DevicePrefs>): DevicePrefs {
  const next = { ...loadDevicePrefs(), ...partial };
  try {
    localStorage.setItem(KEY, JSON.stringify(next));
  } catch {
    /* private mode */
  }
  return next;
}

export function getDeviceId() {
  return loadDevicePrefs().deviceId;
}

export function setManualOutput(output: OutputTarget) {
  saveDevicePrefs({ outputManual: output });
}

export function discordOptionVisible(voice: VoiceState | null) {
  if (!voice) return true;
  return voice.discord_enabled !== false;
}

export function discordReady(voice: VoiceState | null) {
  return !!(voice && voice.discord_enabled && voice.linked && voice.in_voice);
}

/** Playback target: manual override wins; otherwise Discord when enabled+linked+in_voice. */
export function resolveOutput(voice: VoiceState | null, manual: OutputTarget | null = loadDevicePrefs().outputManual): OutputTarget {
  if (voice && voice.discord_enabled === false) return "browser";
  if (manual === "browser") return "browser";
  if (manual === "discord") return "discord";
  return discordReady(voice) ? "discord" : "browser";
}
