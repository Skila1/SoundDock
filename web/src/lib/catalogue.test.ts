import { QueryClient } from "@tanstack/react-query";
import { describe, expect, it } from "vitest";
import { clearCatalogueTracks, patchTracksInCaches, performerNames, removeTracksFromCaches } from "./catalogue";

describe("catalogue caches", () => {
  it("drops deleted tracks from home and tracks immediately", () => {
    const qc = new QueryClient();
    qc.setQueryData(["home"], {
      continue: [
        { id: "keep", title: "Keep" },
        { track_id: "gone", title: "Gone" }
      ],
      recently_added: [{ id: "gone" }],
      most_played: [{ id: "keep" }]
    });
    qc.setQueryData(["tracks"], [{ id: "keep" }, { id: "gone" }]);
    qc.setQueryData(["search", "bell"], { results: [{ type: "track", id: "gone" }, { type: "album", id: "gone" }] });
    qc.setQueryData(["favourites"], [{ type: "track", id: "gone" }, { type: "album", id: "a1" }]);
    qc.setQueryData(["album", "alb"], { id: "alb", tracks: [{ id: "keep" }, { id: "gone" }] });

    removeTracksFromCaches(qc, ["gone"]);

    expect(qc.getQueryData(["home"])).toEqual({
      continue: [{ id: "keep", title: "Keep" }],
      recently_added: [],
      most_played: [{ id: "keep" }]
    });
    expect(qc.getQueryData(["tracks"])).toEqual([{ id: "keep" }]);
    expect(qc.getQueryData(["search", "bell"])).toEqual({ results: [{ type: "album", id: "gone" }] });
    expect(qc.getQueryData(["favourites"])).toEqual([{ type: "album", id: "a1" }]);
    expect(qc.getQueryData(["album", "alb"])).toEqual({ id: "alb", tracks: [{ id: "keep" }] });
  });

  it("clears catalogue lists for delete-all", () => {
    const qc = new QueryClient();
    qc.setQueryData(["home"], { continue: [{ id: "a" }], recently_added: [{ id: "b" }], most_played: [{ id: "c" }] });
    qc.setQueryData(["tracks"], [{ id: "a" }]);
    clearCatalogueTracks(qc);
    expect(qc.getQueryData(["home"])).toEqual({ continue: [], recently_added: [], most_played: [] });
    expect(qc.getQueryData(["tracks"])).toEqual([]);
  });

  it("patches metadata on cached tracks", () => {
    const qc = new QueryClient();
    qc.setQueryData(["tracks"], [{ id: "a", genre: "Pop" }, { id: "b", genre: "Jazz" }]);
    qc.setQueryData(["home"], { continue: [{ id: "a", year: 1979 }], recently_added: [], most_played: [] });
    patchTracksInCaches(qc, ["a"], { genre: "Disco", year: 1979 });
    expect(qc.getQueryData(["tracks"])).toEqual([
      { id: "a", genre: "Disco", year: 1979 },
      { id: "b", genre: "Jazz" }
    ]);
    expect((qc.getQueryData(["home"]) as { continue: { year: number }[] }).continue[0].year).toBe(1979);
  });

  it("covers the favourites and my library rails on Home", () => {
    const qc = new QueryClient();
    qc.setQueryData(["home"], {
      favourites: [{ id: "a", title: "Old" }, { id: "b" }],
      my_library: [{ id: "a", title: "Old" }],
      continue: []
    });
    patchTracksInCaches(qc, ["a"], { title: "New", artist: "Someone" });
    const home = qc.getQueryData(["home"]) as Record<string, { id: string; title?: string; artist?: string }[]>;
    expect(home.favourites[0]).toEqual({ id: "a", title: "New", artist: "Someone" });
    expect(home.my_library[0].title).toBe("New");
    removeTracksFromCaches(qc, ["a"]);
    const after = qc.getQueryData(["home"]) as Record<string, unknown[]>;
    expect(after.favourites).toEqual([{ id: "b" }]);
    expect(after.my_library).toEqual([]);
  });

  it("builds the editable artist field without composers", () => {
    expect(
      performerNames([
        { name: "Main", role: "primary" },
        { name: "Writer", role: "composer" },
        { name: "Guest", role: "featured" },
        { name: "Legacy" }
      ])
    ).toBe("Main, Guest, Legacy");
    expect(performerNames(undefined)).toBe("");
  });
});
