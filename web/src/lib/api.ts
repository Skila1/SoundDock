async function parse(r: Response) {
  if (!r.ok) {
    let msg = r.statusText;
    try {
      const b = await r.json();
      msg = b.message || b.code || msg;
    } catch {
      /* ignore */
    }
    const err = new Error(msg) as Error & { status: number };
    err.status = r.status;
    throw err;
  }
  if (r.status === 204) return null;
  const ct = r.headers.get("content-type") || "";
  if (ct.includes("application/json")) return r.json();
  return r.text();
}

export const api = {
  get: <T = any>(p: string) => fetch(p, { credentials: "include" }).then(parse) as Promise<T>,
  post: <T = any>(p: string, body?: unknown) =>
    fetch(p, {
      method: "POST",
      credentials: "include",
      headers: body instanceof FormData || body instanceof Blob ? undefined : { "Content-Type": "application/json" },
      body: body instanceof Blob || body instanceof FormData ? (body as BodyInit) : body ? JSON.stringify(body) : undefined
    }).then(parse) as Promise<T>,
  put: <T = any>(p: string, body?: unknown) =>
    fetch(p, { method: "PUT", credentials: "include", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) }).then(parse) as Promise<T>,
  patch: <T = any>(p: string, body?: unknown, headers?: HeadersInit) =>
    fetch(p, { method: "PATCH", credentials: "include", headers: { "Content-Type": "application/json", ...headers }, body: body instanceof Blob ? body : JSON.stringify(body) }).then(parse) as Promise<T>,
  del: <T = any>(p: string) => fetch(p, { method: "DELETE", credentials: "include" }).then(parse) as Promise<T>
};

export const streamUrl = (id: string, quality = "original") => `/api/v1/tracks/${id}/stream?quality=${quality}`;
