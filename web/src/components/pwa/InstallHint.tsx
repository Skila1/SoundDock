import { useEffect, useState } from "react";
import { Button } from "@/components/ui/button";

function isAppleMobile() {
  const ua = navigator.userAgent || "";
  if (/iPad|iPhone|iPod/.test(ua)) return true;
  return navigator.platform === "MacIntel" && navigator.maxTouchPoints > 1;
}

function isStandalone() {
  const nav = navigator as Navigator & { standalone?: boolean };
  return nav.standalone === true || window.matchMedia("(display-mode: standalone)").matches;
}

export function InstallHint() {
  const [show, setShow] = useState(false);
  useEffect(() => {
    if (localStorage.getItem("sd-ios-install") === "1") return;
    if (isAppleMobile() && !isStandalone()) setShow(true);
  }, []);
  if (!show) return null;
  return (
    <div className="flex items-center justify-between gap-3 border-b border-border bg-surface-2 px-4 py-2 text-sm md:hidden">
      <span>Install SoundDock from Share, then Add to Home Screen.</span>
      <Button
        size="sm"
        variant="ghost"
        onClick={() => {
          localStorage.setItem("sd-ios-install", "1");
          setShow(false);
        }}
      >
        Dismiss
      </Button>
    </div>
  );
}
