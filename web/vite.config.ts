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
      registerType: "autoUpdate",
      includeAssets: ["logo.png", "favicon.svg"],
      workbox: {
        navigateFallback: "index.html",
        navigateFallbackDenylist: [
          /^\/api\//,
          /^\/rest\//,
          /^\/healthz/,
          /^\/readyz/,
          /^\/metrics/,
          /^\/openapi/
        ],
        runtimeCaching: [
          {
            urlPattern: ({ url }) =>
              url.pathname.startsWith("/api/") ||
              url.pathname.startsWith("/rest/") ||
              url.pathname === "/healthz" ||
              url.pathname === "/readyz",
            handler: "NetworkOnly"
          }
        ]
      },
      manifest: {
        name: "SoundDock",
        short_name: "SoundDock",
        description: "Self-hosted music library",
        theme_color: "#0c1117",
        background_color: "#0c1117",
        display: "standalone",
        start_url: "/",
        icons: [
          { src: "/logo.png", sizes: "192x192", type: "image/png", purpose: "any" },
          { src: "/logo.png", sizes: "512x512", type: "image/png", purpose: "any" }
        ]
      }
    })
  ],
  resolve: { alias: { "@": path.resolve(__dirname, "./src") } },
  server: { proxy: { "/api": "http://127.0.0.1:8080", "/healthz": "http://127.0.0.1:8080" } },
  build: { outDir: "dist", emptyOutDir: true }
});
