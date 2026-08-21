const STORAGE_KEY = "sounddock.device_id";

export function deviceId(): string {
  try {
    const existing = localStorage.getItem(STORAGE_KEY);
    if (existing && /^[A-Za-z0-9_:-]{1,128}$/.test(existing)) return existing;
    const bytes = new Uint8Array(4);
    crypto.getRandomValues(bytes);
    const id = `browser-${Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join("")}`;
    localStorage.setItem(STORAGE_KEY, id);
    return id;
  } catch {
    return "browser-1";
  }
}
