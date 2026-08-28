import { describe, expect, it, vi } from "vitest";
import { createCommandClient, newCommandId } from "@/stores/commandClient";

describe("commandClient", () => {
  it("sends a UUID command_id on control", async () => {
    const send = vi.fn(async (body) => body);
    const client = createCommandClient(send);
    await client.control("pause");
    expect(send).toHaveBeenCalledOnce();
    const body = send.mock.calls[0][0];
    expect(body.action).toBe("pause");
    expect(body.command_id).toMatch(
      /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i
    );
    expect(body.extra.command_id).toBe(body.command_id);
  });

  it("replays a duplicate command_id with the same payload", async () => {
    const send = vi.fn(async () => ({ state_revision: 3 }));
    const client = createCommandClient(send);
    const id = newCommandId();
    const a = await client.control("volume", { volume: 0.4, command_id: id });
    const b = await client.control("volume", { volume: 0.4, command_id: id });
    expect(send).toHaveBeenCalledOnce();
    expect(a).toEqual(b);
    expect(b).toEqual({ state_revision: 3 });
  });

  it("joins an in-flight duplicate instead of sending twice", async () => {
    let resolveSend: (v: unknown) => void = () => undefined;
    const send = vi.fn(
      () =>
        new Promise((resolve) => {
          resolveSend = resolve;
        })
    );
    const client = createCommandClient(send);
    const id = "cmd-dup";
    const p1 = client.control("pause", { command_id: id });
    const p2 = client.control("pause", { command_id: id });
    expect(send).toHaveBeenCalledOnce();
    resolveSend({ ok: true });
    expect(await p1).toEqual(await p2);
  });
});
