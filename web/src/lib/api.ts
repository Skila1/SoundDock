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
    if (r.status === 401) {
      const { queryClient } = await import("@/app/providers");
      queryClient.removeQueries({ queryKey: ["me"] });
    }
    throw err;
  }
  if (r.status === 204) return null;
  const ct = r.headers.get("content-type") || "";
  if (ct.includes("application/json")) return r.json();
  return r.text();
}

let csrfTokenRequest: Promise<string> | undefined;

export function getCSRFToken() {
  if (!csrfTokenRequest) {
    csrfTokenRequest = fetch("/api/v1/auth/csrf", { credentials: "include" })
      .then(async (response) => {
        if (!response.ok) throw new Error("Could not obtain upload security token");
        const payload = await response.json() as { csrf: string };
        return payload.csrf;
      })
      .catch((error) => {
        csrfTokenRequest = undefined;
        throw error;
      });
  }
  return csrfTokenRequest;
}

async function csrfHeaders(headers?: HeadersInit) {
  return { ...(headers || {}), "X-CSRF-Token": await getCSRFToken() };
}

export const api = {
  get: <T = any>(p: string) => fetch(p, { credentials: "include" }).then(parse) as Promise<T>,
  post: async <T = any>(p: string, body?: unknown) =>
    fetch(p, {
      method: "POST",
      credentials: "include",
      headers: await csrfHeaders(body instanceof FormData || body instanceof Blob ? undefined : { "Content-Type": "application/json" }),
      body: body instanceof Blob || body instanceof FormData ? (body as BodyInit) : body ? JSON.stringify(body) : undefined
    }).then(parse) as Promise<T>,
  put: async <T = any>(p: string, body?: unknown) =>
    fetch(p, { method: "PUT", credentials: "include", headers: await csrfHeaders({ "Content-Type": "application/json" }), body: JSON.stringify(body) }).then(parse) as Promise<T>,
  patch: async <T = any>(p: string, body?: unknown, headers?: HeadersInit) =>
    fetch(p, { method: "PATCH", credentials: "include", headers: await csrfHeaders({ "Content-Type": "application/json", ...headers }), body: body instanceof Blob ? body : JSON.stringify(body) }).then(parse) as Promise<T>,
  del: async <T = any>(p: string, body?: unknown) =>
    fetch(p, {
      method: "DELETE",
      credentials: "include",
      headers: await csrfHeaders(body ? { "Content-Type": "application/json" } : undefined),
      body: body ? JSON.stringify(body) : undefined
    }).then(parse) as Promise<T>
};

export const streamUrl = (id: string, quality = "original") => `/api/v1/tracks/${id}/stream?quality=${quality}`;

export function streamWithOffline(id: string, offlineToken: string, quality = "original") {
  return `/api/v1/tracks/${id}/stream?quality=${quality}&offline_token=${encodeURIComponent(offlineToken)}`;
}
