import { describe, expect, it } from "vitest";
import { mergePresence, normalizeParticipant } from "@/stores/sseClient";

describe("normalizeParticipant", () => {
  it("rejects empty objects", () => {
    expect(normalizeParticipant({})).toBeNull();
    expect(normalizeParticipant(null)).toBeNull();
  });

  it("keeps a name when user_id is missing", () => {
    const p = normalizeParticipant({ display_name: "Pixel" });
    expect(p?.user_id).toBe("Pixel");
    expect(p?.display_name).toBe("Pixel");
  });

  it("coerces numeric ids", () => {
    const p = normalizeParticipant({ user_id: 123, display_name: "A" });
    expect(p?.user_id).toBe("123");
  });
});

describe("mergePresence", () => {
  it("does not treat a listeners wrapper as one person", () => {
    const prev = [{ user_id: "a", display_name: "A", avatar_url: null, source: "web" as const }];
    const next = mergePresence(prev, { listeners: [] });
    expect(next).toEqual([]);
  });

  it("does not invent a person from a malformed listeners object", () => {
    const prev = [{ user_id: "a", display_name: "A", avatar_url: null, source: "web" as const }];
    const next = mergePresence(prev, { listeners: { id: "queue" } });
    expect(next.find((p) => p.user_id === "queue")).toBeUndefined();
  });
});
