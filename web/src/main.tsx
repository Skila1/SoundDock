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
