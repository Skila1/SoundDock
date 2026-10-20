import { describe, expect, it } from "vitest";
import { moveSection, orderSections } from "./homeOrder";

const secs = ["favourites", "playlists", "recent", "full"].map((id) => ({ id }));
const ids = (xs: { id: string }[]) => xs.map((x) => x.id);

describe("orderSections", () => {
  it("keeps the default order when nothing is saved", () => {
    expect(ids(orderSections(secs))).toEqual(["favourites", "playlists", "recent", "full"]);
  });

  it("applies the saved order and drops sections that no longer exist", () => {
    expect(ids(orderSections(secs, ["full", "recent", "gone", "playlists", "favourites"]))).toEqual(["full", "recent", "playlists", "favourites"]);
  });

  it("slots new sections in after their default neighbour", () => {
    // Saved before Your Playlists and Full Library existed.
    const saved = ["favourites", "added", "library", "recent", "most"];
    expect(ids(orderSections(secs, saved))).toEqual(["favourites", "playlists", "recent", "full"]);
    expect(ids(orderSections(secs, ["recent", "favourites"]))).toEqual(["recent", "favourites", "playlists", "full"]);
  });

  it("puts a new first section at the top", () => {
    expect(ids(orderSections(secs, ["recent", "playlists"]))).toEqual(["favourites", "recent", "playlists", "full"]);
  });
});

describe("moveSection", () => {
  const order = ["favourites", "playlists", "recent", "full"];

  it("moves a section before another", () => {
    expect(moveSection(order, "full", "playlists")).toEqual(["favourites", "full", "playlists", "recent"]);
  });

  it("moves a section to the end", () => {
    expect(moveSection(order, "favourites", null)).toEqual(["playlists", "recent", "full", "favourites"]);
  });

  it("ignores a drop onto itself", () => {
    expect(moveSection(order, "recent", "recent")).toBe(order);
  });
});
