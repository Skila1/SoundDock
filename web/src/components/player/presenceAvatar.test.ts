import { describe, expect, it } from "vitest";
import { avatarDisplaySrc, looksAnimatedAvatar, staticAvatarUrl } from "./presenceAvatar";

describe("presenceAvatar", () => {
  it("treats discord gif avatars as animated", () => {
    expect(looksAnimatedAvatar("https://cdn.discordapp.com/avatars/1/a_abc.gif?size=32")).toBe(true);
    expect(looksAnimatedAvatar("https://cdn.discordapp.com/avatars/1/a_abc.webp")).toBe(true);
    expect(looksAnimatedAvatar("https://cdn.discordapp.com/avatars/1/deadbeef.png")).toBe(false);
  });

  it("maps animated urls to a still png", () => {
    expect(staticAvatarUrl("https://cdn.discordapp.com/avatars/1/a_abc.gif?size=32")).toBe(
      "https://cdn.discordapp.com/avatars/1/a_abc.png?size=32"
    );
  });

  it("keeps the live gif only while the page is active", () => {
    const gif = "https://cdn.discordapp.com/avatars/1/a_abc.gif";
    expect(avatarDisplaySrc(gif, true)).toBe(gif);
    expect(avatarDisplaySrc(gif, false)).toBe("https://cdn.discordapp.com/avatars/1/a_abc.png");
  });
});
