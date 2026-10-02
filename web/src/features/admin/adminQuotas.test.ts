import { describe, expect, it } from "vitest";
import { mergeQuotas, overrideFor, type Quotas } from "./adminQuotas";
import { bytesFromParts, capLabel, splitBytes } from "./adminFormat";

const GB = 1024 ** 3;

const cur: Quotas = {
  default_user_bytes: 5 * GB,
  default_library_bytes: 0,
  users: [{ user_id: "u1", max_bytes: GB }, { user_id: "u2", max_bytes: 2 * GB }],
  libraries: [{ library_id: "l1", max_bytes: 100 * GB }],
  user_usage: { u1: 10 }
};

describe("quota merge", () => {
  it("changes one user override and keeps everything else", () => {
    const out = mergeQuotas(cur, { user: { id: "u1", bytes: 3 * GB } });
    expect(out.users).toEqual([{ user_id: "u2", max_bytes: 2 * GB }, { user_id: "u1", max_bytes: 3 * GB }]);
    expect(out.libraries).toEqual(cur.libraries);
    expect(out.default_user_bytes).toBe(5 * GB);
    expect(out).not.toHaveProperty("user_usage");
  });

  it("removes an override when set back to the default", () => {
    const out = mergeQuotas(cur, { user: { id: "u2", bytes: null } });
    expect(out.users.map((u) => u.user_id)).toEqual(["u1"]);
  });

  it("keeps an explicit unlimited (0) override", () => {
    const out = mergeQuotas(cur, { library: { id: "l2", bytes: 0 } });
    expect(out.libraries).toContainEqual({ library_id: "l2", max_bytes: 0 });
  });

  it("updates defaults", () => {
    const out = mergeQuotas(cur, { defaultLibraryBytes: 7, defaultUserBytes: 0 });
    expect(out.default_library_bytes).toBe(7);
    expect(out.default_user_bytes).toBe(0);
    expect(out.users).toHaveLength(2);
  });

  it("finds overrides", () => {
    expect(overrideFor(cur.users, (r) => r.user_id === "u2")).toBe(2 * GB);
    expect(overrideFor(cur.users, (r) => r.user_id === "nope")).toBeNull();
  });
});

describe("byte formatting", () => {
  it("converts units", () => {
    expect(bytesFromParts(1.5, "GB")).toBe(1.5 * GB);
    expect(bytesFromParts(-1, "GB")).toBe(0);
    expect(splitBytes(2 * 1024 ** 4)).toEqual({ value: 2, unit: "TB" });
    expect(splitBytes(512 * 1024 ** 2)).toEqual({ value: 512, unit: "MB" });
    expect(capLabel(0)).toBe("Unlimited");
    expect(capLabel(10 * GB)).toBe("10 GB");
  });
});
