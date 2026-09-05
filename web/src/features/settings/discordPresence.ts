import { api } from "@/lib/api";
import { usePlayer } from "@/stores/player";

const STORAGE_KEY = "sd-discord-presence";
const ACTIVITY_DEBOUNCE_MS = 400;

/** Discord desktop RPC. Only 6463 - walking 6463-6472 floods the console when Discord is closed. */
const RPC_PORT = 6463;

let subscribed = false;
let ws: WebSocket | null = null;
let clientId = "";
let opening: Promise<WebSocket | null> | null = null;
let rpcUnavailable = false;
let lastActivityKey = "";
let activityTimer: number | undefined;
let unsub: (() => void) | null = null;

type Activity = {
  title: string;
  artist?: string;
  album?: string;
  playing: boolean;
  startedAt?: number;
};

function rpcFrame(cmd: string, args: Record<string, unknown>) {
  return JSON.stringify({ cmd, args, nonce: crypto.randomUUID() });
}

function activityKey(act: Activity | null) {
  if (!act) return "clear";
  return [act.title, act.artist || "", act.playing ? "1" : "0", act.startedAt ? Math.floor(act.startedAt / 5000) : "0"].join("|");
}

/** Never close() a CONNECTING socket - Chrome logs that as a failed handshake. */
function abandon(socket: WebSocket) {
  socket.onopen = null;
  socket.onerror = null;
  socket.onclose = null;
  socket.onmessage = null;
  if (socket.readyState === WebSocket.OPEN || socket.readyState === WebSocket.CLOSING) {
    try {
      socket.close();
    } catch {
      /* ignore */
    }
  } else if (socket.readyState === WebSocket.CONNECTING) {
    socket.addEventListener("open", () => {
      try {
        socket.close();
      } catch {
        /* ignore */
      }
    });
  }
}

function connect(id: string): Promise<WebSocket> {
  return new Promise((resolve, reject) => {
    let settled = false;
    const socket = new WebSocket(`ws://127.0.0.1:${RPC_PORT}/?v=1&client_id=${encodeURIComponent(id)}&encoding=json`);
    const finish = (ok: boolean, err?: Error) => {
      if (settled) return;
      settled = true;
      window.clearTimeout(t);
      if (ok) resolve(socket);
      else {
        abandon(socket);
        reject(err || new Error("ws error"));
      }
    };
    const t = window.setTimeout(() => finish(false, new Error("timeout")), 800);
    socket.onopen = () => finish(true);
    socket.onerror = () => finish(false, new Error("ws error"));
  });
}

function waitReady(socket: WebSocket, ms = 2000): Promise<void> {
  if (socket.readyState !== WebSocket.OPEN) return Promise.reject(new Error("closed"));
  return new Promise((resolve, reject) => {
    const t = window.setTimeout(() => {
      socket.removeEventListener("message", onMsg);
      reject(new Error("ready timeout"));
    }, ms);
    const onMsg = (ev: MessageEvent) => {
      try {
        const payload = typeof ev.data === "string" ? JSON.parse(ev.data) : ev.data;
        if (payload?.evt === "READY" || payload?.cmd === "DISPATCH") {
          window.clearTimeout(t);
          socket.removeEventListener("message", onMsg);
          resolve();
        }
      } catch {
        /* ignore non-JSON */
      }
    };
    socket.addEventListener("message", onMsg);
  });
}

async function openRPC(id: string) {
  if (rpcUnavailable) return null;
  if (ws && ws.readyState === WebSocket.OPEN) return ws;
  if (opening) return opening;
  opening = (async () => {
    try {
      const socket = await connect(id);
      try {
        await waitReady(socket);
      } catch {
        /* some Discord builds accept SET_ACTIVITY without READY */
      }
      ws = socket;
      socket.onclose = () => {
        if (ws === socket) ws = null;
      };
      socket.onerror = () => {
        if (ws === socket) {
          ws = null;
          rpcUnavailable = true;
        }
      };
      return socket;
    } catch {
      rpcUnavailable = true;
      return null;
    }
  })().finally(() => {
    opening = null;
  });
  return opening;
}

async function setActivity(act: Activity | null) {
  if (!clientId || !enabled()) return;
  if (rpcUnavailable && ws?.readyState !== WebSocket.OPEN) return;
  const key = activityKey(act);
  if (key === lastActivityKey && ws?.readyState === WebSocket.OPEN) return;
  const socket = await openRPC(clientId);
  if (!socket || socket.readyState !== WebSocket.OPEN) return;
  lastActivityKey = key;
  if (!act || !act.playing || !act.title) {
    socket.send(rpcFrame("SET_ACTIVITY", { pid: 0, activity: null }));
    return;
  }
  socket.send(
    rpcFrame("SET_ACTIVITY", {
      pid: 0,
      activity: {
        type: 2,
        details: act.title.slice(0, 128),
        state: (act.artist || "SoundDock").slice(0, 128),
        timestamps: act.startedAt ? { start: act.startedAt } : undefined,
        assets: { large_text: act.album || "SoundDock" }
      }
    })
  );
}

function enabled() {
  try {
    return localStorage.getItem(STORAGE_KEY) === "1";
  } catch {
    return false;
  }
}

function activityFromPlayer(): Activity | null {
  const s = usePlayer.getState();
  const t = s.current;
  if (!t) return null;
  return {
    title: t.title,
    artist: t.artists?.map((a) => a.name).join(", ") || t.artist,
    album: t.album,
    playing: s.playing,
    startedAt: s.playing ? Date.now() - (s.position || 0) : undefined
  };
}

function queueActivitySync() {
  if (!enabled() || rpcUnavailable) return;
  if (activityTimer) window.clearTimeout(activityTimer);
  activityTimer = window.setTimeout(() => {
    setActivity(activityFromPlayer()).catch(() => undefined);
  }, ACTIVITY_DEBOUNCE_MS);
}

function attachPlayer() {
  if (subscribed) return;
  subscribed = true;
  unsub = usePlayer.subscribe((s, prev) => {
    if (!enabled() || rpcUnavailable) return;
    if (s.current?.id === prev.current?.id && s.playing === prev.playing && s.current?.title === prev.current?.title) {
      return;
    }
    queueActivitySync();
  });
}

export function setDiscordPresenceEnabled(on: boolean) {
  try {
    localStorage.setItem(STORAGE_KEY, on ? "1" : "0");
  } catch {
    /* ignore */
  }
  lastActivityKey = "";
  if (on) {
    rpcUnavailable = false;
    void ensureDiscordPresence().then(() => syncDiscordPresence());
  } else {
    if (ws?.readyState === WebSocket.OPEN) {
      setActivity(null).catch(() => undefined);
    }
    if (ws) abandon(ws);
    ws = null;
    rpcUnavailable = true;
  }
}

export function isDiscordPresenceEnabled() {
  return enabled();
}

export async function ensureDiscordPresence() {
  try {
    if (!clientId) {
      const st = await api.get<{ application_id?: string }>("/api/v1/me/discord/voice-state");
      clientId = st.application_id || "";
    }
    const sc = await api.get<{ presence_enabled?: boolean }>("/api/v1/me/scrobble").catch(() => ({ presence_enabled: false }));
    if (sc.presence_enabled) {
      try {
        localStorage.setItem(STORAGE_KEY, "1");
      } catch {
        /* ignore */
      }
    }
  } catch {
    /* unauthenticated or bot not configured */
  }
  attachPlayer();
  // Do not open localhost RPC on boot. Discord desktop is ws://127.0.0.1:6463 on this
  // machine; failed probes show up as console errors even when the app is behind cloudflared.
}

export async function syncDiscordPresence() {
  if (!enabled() || rpcUnavailable) return;
  lastActivityKey = "";
  await setActivity(activityFromPlayer());
}

export function resetDiscordPresenceForTests() {
  if (activityTimer) window.clearTimeout(activityTimer);
  activityTimer = undefined;
  unsub?.();
  unsub = null;
  subscribed = false;
  if (ws) abandon(ws);
  ws = null;
  opening = null;
  clientId = "";
  rpcUnavailable = false;
  lastActivityKey = "";
}
