import { describe, expect, it } from "vitest";
import { orderPresence, presenceLabel, queueReorderTarget, showQueueInsertLine } from "@/components/player/QueuePanel";

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

describe("queue insert line", () => {
  it("shows a marker on the hovered index and not on the dragged row", () => {
    expect(showQueueInsertLine(2, 5, 5)).toBe(true);
    expect(showQueueInsertLine(2, 5, 2)).toBe(false);
    expect(showQueueInsertLine(2, 2, 2)).toBe(false);
    expect(showQueueInsertLine(-1, 3, 3)).toBe(false);
  });

  it("maps a trailing drop onto the last slot", () => {
    expect(queueReorderTarget(1, 4, 4)).toBe(3);
    expect(queueReorderTarget(3, 4, 4)).toBeNull();
    expect(queueReorderTarget(2, 2, 5)).toBeNull();
    expect(queueReorderTarget(0, 2, 5)).toBe(2);
  });
});

