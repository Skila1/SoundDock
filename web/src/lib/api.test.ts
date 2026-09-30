import { afterEach, describe, expect, it, vi } from "vitest";
import { getCSRFToken, handleUnauthorized } from "./api";

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("getCSRFToken", () => {
  it("shares one token request across concurrent upload workers", async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ csrf: "stable-token" })
    });
    vi.stubGlobal("fetch", fetchMock);

    const tokens = await Promise.all([getCSRFToken(), getCSRFToken(), getCSRFToken()]);

    expect(tokens).toEqual(["stable-token", "stable-token", "stable-token"]);
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });
});

describe("handleUnauthorized", () => {
  it("keeps the me query when /me itself returns 401 so observers do not refetch", async () => {
    const { queryClient } = await import("@/app/providers");
    queryClient.setQueryData(["me"], { id: "u1" });
    await handleUnauthorized("/api/v1/me");
    expect(queryClient.getQueryData(["me"])).toEqual({ id: "u1" });
    queryClient.clear();
  });

  it("drops the cached user when another request returns 401", async () => {
    const { queryClient } = await import("@/app/providers");
    queryClient.setQueryData(["me"], { id: "u1" });
    await handleUnauthorized("/api/v1/tracks?page=1");
    expect(queryClient.getQueryData(["me"])).toBeUndefined();
    queryClient.clear();
  });
});
