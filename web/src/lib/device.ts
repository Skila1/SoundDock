const KEY = "sd-device";
const RENDERER_ID_KEY = "sd-renderer-id";
const RENDERER_GEN_KEY = "sd-renderer-generation";
export const RENDERER_CHANNEL = "sd-renderer";

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

let memoryRendererId = "";

function newRendererId() {
  try {
    return crypto.randomUUID();
  } catch {
    return `tab-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`;
  }
}

/** Per-tab browser renderer identity. sessionStorage only - never localStorage. */
export function getTabRendererId(): string {
  try {
    if (typeof sessionStorage === "undefined") {
      if (!memoryRendererId) memoryRendererId = newRendererId();
      return memoryRendererId;
    }
    let id = sessionStorage.getItem(RENDERER_ID_KEY);
    if (!id) {
      id = newRendererId();
      sessionStorage.setItem(RENDERER_ID_KEY, id);
      if (!sessionStorage.getItem(RENDERER_GEN_KEY)) sessionStorage.setItem(RENDERER_GEN_KEY, "1");
    }
    return id;
  } catch {
    if (!memoryRendererId) memoryRendererId = newRendererId();
    return memoryRendererId;
  }
}

export function getTabRendererGeneration(): number {
  try {
    const n = Number(sessionStorage.getItem(RENDERER_GEN_KEY) || "1");
    return Number.isFinite(n) && n > 0 ? Math.floor(n) : 1;
  } catch {
    return 1;
  }
}

export type RendererChannelMessage = {
  type: "stop-request";
  renderer_id: string;
};

let rendererChannel: BroadcastChannel | null = null;
let rendererStopHandler: (() => void) | null = null;

function ensureRendererChannel(): BroadcastChannel | null {
  if (typeof BroadcastChannel === "undefined") return null;
  if (!rendererChannel) {
    rendererChannel = new BroadcastChannel(RENDERER_CHANNEL);
    rendererChannel.onmessage = (ev: MessageEvent<RendererChannelMessage>) => {
      const data = ev.data;
      if (data?.type === "stop-request" && data.renderer_id && data.renderer_id !== getTabRendererId()) {
        rendererStopHandler?.();
      }
    };
  }
  return rendererChannel;
}

/** Tab B asks A to stop HTMLAudio before CAS-acquiring the browser lease. */
export function askRendererTabsToStop() {
  ensureRendererChannel()?.postMessage({ type: "stop-request", renderer_id: getTabRendererId() } satisfies RendererChannelMessage);
}

export function subscribeRendererChannel(onStopRequest: () => void) {
  rendererStopHandler = onStopRequest;
  ensureRendererChannel();
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

/** Discord is the default. Browser wins only if the user locked it or no seeable VC. */
export function resolveOutput(voice: VoiceState | null, manual: OutputTarget | null = loadDevicePrefs().outputManual): OutputTarget {
  if (voice && voice.discord_enabled === false) return "browser";
  if (manual === "browser") return "browser";
  if (!voice) return "discord";
  return discordReady(voice) ? "discord" : "browser";
}
