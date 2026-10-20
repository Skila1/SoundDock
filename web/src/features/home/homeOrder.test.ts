import { describe, expect, it } from "vitest";
import { moveSection, orderSections } from "./homeOrder";

const secs = ["favourites", "library", "recent", "added", "most"].map((id) => ({ id }));
const ids = (xs: { id: string }[]) => xs.map((x) => x.id);

describe("orderSections", () => {
  it("keeps the default order when nothing is saved", () => {
    expect(ids(orderSections(secs))).toEqual(["favourites", "library", "recent", "added", "most"]);
  });

  it("applies the saved order and keeps unknown sections after it", () => {
    expect(ids(orderSections(secs, ["most", "recent", "gone"]))).toEqual(["most", "recent", "favourites", "library", "added"]);
  });
});

describe("moveSection", () => {
  const order = ["favourites", "library", "recent", "added", "most"];

  it("moves a section before another", () => {
    expect(moveSection(order, "most", "library")).toEqual(["favourites", "most", "library", "recent", "added"]);
  });

  it("moves a section to the end", () => {
    expect(moveSection(order, "favourites", null)).toEqual(["library", "recent", "added", "most", "favourites"]);
  });

  it("ignores a drop onto itself", () => {
    expect(moveSection(order, "recent", "recent")).toBe(order);
  });
});
