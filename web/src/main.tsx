import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { BrowserRouter } from "react-router-dom";
import { registerSW } from "virtual:pwa-register";
import { toast } from "sonner";
import { App } from "./App";
import { ErrorBoundary } from "./app/ErrorBoundary";
import "./index.css";

type BeforeInstallPromptEvent = Event & {
  prompt: () => Promise<void>;
  userChoice: Promise<{ outcome: "accepted" | "dismissed" }>;
};

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

window.addEventListener("beforeinstallprompt", (event) => {
  event.preventDefault();
  const install = event as BeforeInstallPromptEvent;
  window.setTimeout(() => {
    toast("Install SoundDock", {
      description: "Add the app for offline playback of tracks you save.",
      action: {
        label: "Install",
        onClick: () => void install.prompt()
      },
      duration: 20_000
    });
  }, 400);
});
