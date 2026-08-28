import { describe, expect, it } from "vitest";
import { hasPerm } from "./perms";

describe("hasPerm", () => {
  it("allows admins", () => {
    expect(hasPerm({ is_admin: true, permissions: [] }, "providers.connect")).toBe(true);
  });
  it("checks the named permission", () => {
    expect(hasPerm({ permissions: ["providers.connect"] }, "providers.connect")).toBe(true);
    expect(hasPerm({ permissions: ["tracks.read"] }, "providers.connect")).toBe(false);
  });
  it("rejects missing users", () => {
    expect(hasPerm(null, "providers.connect")).toBe(false);
  });
});
