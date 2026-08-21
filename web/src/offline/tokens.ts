import { deviceId } from "./device";

export type OfflineToken = {
  token: string;
  track_id: string;
  device_id: string;
  user_id: string;
  expires_at: string;
  url: string;
};

async function parse(r: Response) {
  if (!r.ok) {
    let msg = r.statusText;
    try {
      const b = await r.json();
      msg = b.message || b.code || msg;
    } catch {
      /* ignore */
    }
    throw new Error(msg);
  }
  if (r.status === 204) return null;
  return r.json();
}

export async function mintOfflineToken(trackId: string): Promise<OfflineToken> {
  const r = await fetch("/api/v1/me/offline/tokens", {
    method: "POST",
    credentials: "include",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ track_id: trackId, device_id: deviceId() })
  });
  return parse(r) as Promise<OfflineToken>;
}

export async function revokeOfflineTokens(): Promise<void> {
  const r = await fetch("/api/v1/me/offline/tokens", {
    method: "DELETE",
    credentials: "include",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ device_id: deviceId() })
  });
  await parse(r);
}
