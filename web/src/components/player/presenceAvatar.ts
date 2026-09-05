/** True when this tab is visible and the window has focus. */
export function pageIsActive(): boolean {
  if (typeof document === "undefined") return true;
  return document.visibilityState === "visible" && document.hasFocus();
}

export function looksAnimatedAvatar(url: string): boolean {
  const path = url.split("?")[0].toLowerCase();
  if (path.endsWith(".gif")) return true;
  if (path.endsWith(".webp") && /\/a_[a-f0-9]+/i.test(path)) return true;
  return false;
}

/** Discord (and similar) serve a still PNG at the same avatar path. */
export function staticAvatarUrl(url: string): string {
  if (!looksAnimatedAvatar(url)) return url;
  return url.replace(/\.(gif|webp)(\?|$)/i, ".png$2");
}

export function avatarDisplaySrc(src: string, active: boolean): string {
  if (!src || active) return src;
  return staticAvatarUrl(src);
}
