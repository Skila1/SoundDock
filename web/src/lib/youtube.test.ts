import { describe, expect, it } from "vitest";
import { isYouTubePlaylistQuery, youtubePlaylistId } from "./youtube";

describe("youtube playlist URLs", () => {
  it("reads public playlist pages", () => {
    expect(youtubePlaylistId("https://www.youtube.com/playlist?list=PLabcDEF123")).toBe("PLabcDEF123");
    expect(isYouTubePlaylistQuery("https://music.youtube.com/playlist?list=OLAK5uy_abc")).toBe(true);
  });

  it("treats watch+list user playlists as playlists", () => {
    expect(isYouTubePlaylistQuery("https://www.youtube.com/watch?v=kXYiU_JCYtU&list=PLabcDEF123")).toBe(true);
  });

  it("does not expand mixes or watch-later", () => {
    expect(isYouTubePlaylistQuery("https://www.youtube.com/watch?v=kXYiU_JCYtU&list=RDxyz")).toBe(false);
    expect(youtubePlaylistId("https://www.youtube.com/playlist?list=WL")).toBe("");
    expect(isYouTubePlaylistQuery("https://www.youtube.com/watch?v=kXYiU_JCYtU")).toBe(false);
  });
});
