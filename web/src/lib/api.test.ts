import { afterEach, describe, expect, it, vi } from "vitest";
import { getCSRFToken } from "./api";

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
