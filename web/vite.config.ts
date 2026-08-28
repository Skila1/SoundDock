import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { VitePWA } from "vite-plugin-pwa";
import path from "node:path";

export default defineConfig({
  plugins: [
    react(),
    tailwindcss(),
    VitePWA({
      registerType: "prompt",
      injectRegister: false,
      includeAssets: ["logo.png", "favicon.svg", "manifest.webmanifest"],
      workbox: {
        navigateFallback: "index.html",
        navigateFallbackDenylist: [
          /^\/api\//,
          /^\/rest\//,
          /^\/healthz/,
          /^\/readyz/,
          /^\/metrics/,
          /^\/openapi/
        ]
      },
      manifest: false
    })
  ],
  resolve: { alias: { "@": path.resolve(__dirname, "./src") } },
  server: { proxy: { "/api": "http://127.0.0.1:8080", "/healthz": "http://127.0.0.1:8080" } },
  build: { outDir: "dist", emptyOutDir: true }
});
