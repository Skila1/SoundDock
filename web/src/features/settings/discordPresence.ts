import { api } from "@/lib/api";
import { usePlayer } from "@/stores/player";

const STORAGE_KEY = "sd-discord-presence";
let subscribed = false;
let ws: WebSocket | null = null;
let clientId = "";

type Activity = {
  title: string;
  artist?: string;
  album?: string;
  playing: boolean;
  startedAt?: number;
};

function ports() {
  return Array.from({ length: 10 }, (_, i) => 6463 + i);
}

function rpcFrame(cmd: string, args: Record<string, unknown>) {
  return JSON.stringify({ cmd, args, nonce: crypto.randomUUID() });
}

function connect(port: number, id: string): Promise<WebSocket> {
  return new Promise((resolve, reject) => {
    const socket = new WebSocket(`ws://127.0.0.1:${port}/?v=1&client_id=${encodeURIComponent(id)}&encoding=json`);
    const t = window.setTimeout(() => {
      socket.close();
      reject(new Error("timeout"));
    }, 1500);
    socket.onopen = () => {
      window.clearTimeout(t);
      resolve(socket);
    };
    socket.onerror = () => {
      window.clearTimeout(t);
      reject(new Error("ws error"));
    };
  });
}

async function openRPC(id: string) {
  if (ws && ws.readyState === WebSocket.OPEN) return ws;
  for (const p of ports()) {
    try {
      const socket = await connect(p, id);
      ws = socket;
      socket.onclose = () => {
        if (ws === socket) ws = null;
      };
      return socket;
    } catch {
      /* try next Discord RPC port */
    }
  }
  return null;
}

async function setActivity(act: Activity | null) {
  if (!clientId) return;
  const socket = await openRPC(clientId);
  if (!socket || socket.readyState !== WebSocket.OPEN) return;
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

function attachPlayer() {
  if (subscribed) return;
  subscribed = true;
  usePlayer.subscribe((s) => {
    if (!enabled()) return;
    const t = s.current;
    setActivity(
      t
        ? {
            title: t.title,
            artist: t.artists?.map((a) => a.name).join(", ") || t.artist,
            album: t.album,
            playing: s.playing,
            startedAt: s.playing ? Date.now() - (s.position || 0) : undefined
          }
        : null
    ).catch(() => undefined);
  });
}

export function setDiscordPresenceEnabled(on: boolean) {
  try {
    localStorage.setItem(STORAGE_KEY, on ? "1" : "0");
  } catch {
    /* ignore */
  }
  if (on) {
    void ensureDiscordPresence().then(() => syncDiscordPresence());
  } else {
    setActivity(null).catch(() => undefined);
    ws?.close();
    ws = null;
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
  if (enabled()) await syncDiscordPresence();
}

export async function syncDiscordPresence() {
  if (!enabled()) return;
  const s = usePlayer.getState();
  const t = s.current;
  await setActivity(
    t
      ? {
          title: t.title,
          artist: t.artists?.map((a) => a.name).join(", ") || t.artist,
          album: t.album,
          playing: s.playing,
          startedAt: s.playing ? Date.now() - (s.position || 0) : undefined
        }
      : null
  );
}
