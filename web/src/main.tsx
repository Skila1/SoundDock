import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { BrowserRouter } from "react-router-dom";
import { registerSW } from "virtual:pwa-register";
import { toast } from "sonner";
import { App } from "./App";
import { ErrorBoundary } from "./app/ErrorBoundary";
import "./index.css";

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <ErrorBoundary>
      <BrowserRouter>
        <App />
      </BrowserRouter>
    </ErrorBoundary>
  </StrictMode>
);

const updateSW = registerSW({
  immediate: true,
  onNeedRefresh() {
    toast("A new version of SoundDock is ready", {
      action: { label: "Reload", onClick: () => void updateSW(true) },
      duration: Infinity
    });
  },
  // Long-lived tabs never reload, so look for a new build regularly and when
  // the tab comes back into view instead of only at startup.
  onRegisteredSW(_url, reg) {
    if (!reg) return;
    const check = () => {
      if (navigator.onLine) void reg.update().catch(() => undefined);
    };
    window.setInterval(check, 30 * 60_000);
    document.addEventListener("visibilitychange", () => {
      if (document.visibilityState === "visible") check();
    });
  },
  onOfflineReady() {
    toast.success("SoundDock is ready to work offline");
  }
});

window.addEventListener("beforeinstallprompt", () => {
  // Do not preventDefault - Chrome logs a warning unless we also call prompt().
  if (sessionStorage.getItem("sd-install-toast") === "1") return;
  sessionStorage.setItem("sd-install-toast", "1");
  toast("Install SoundDock from the browser menu for offline playback.");
});
