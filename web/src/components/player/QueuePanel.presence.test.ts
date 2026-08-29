import { describe, expect, it } from "vitest";
import { orderPresence, presenceLabel } from "@/components/player/QueuePanel";

describe("orderPresence", () => {
  it("does not throw on missing display names", () => {
    const rows = orderPresence([
      { user_id: "b", display_name: undefined as unknown as string, avatar_url: null, source: "web" },
      { user_id: "a", display_name: "Ada", avatar_url: null, source: "discord" }
    ]);
    expect(presenceLabel(rows[0])).toBeTruthy();
    expect(() => orderPresence(rows)).not.toThrow();
  });

  it("sorts the current user first", () => {
    const rows = orderPresence(
      [
        { user_id: "b", display_name: "Bee", avatar_url: null, source: "web" },
        { user_id: "me", display_name: "Me", avatar_url: null, source: "web" }
      ],
      "me"
    );
    expect(rows[0].user_id).toBe("me");
  });
});
