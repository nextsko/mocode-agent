import path from "node:path";
import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { visualizer } from "rollup-plugin-visualizer";
import { defineConfig } from "vite";
import { nodePolyfills } from "vite-plugin-node-polyfills";

const shouldAnalyze = process.env.ANALYZE === "true";

// https://vite.dev/config/
export default defineConfig({
  // Use relative paths so assets work under any base path.
  base: "./",
  plugins: [
    nodePolyfills({
      include: ["path", "url"],
    }),
    react(),
    tailwindcss(),
    ...(shouldAnalyze
      ? [
          visualizer({
            brotliSize: true,
            filename: "dist/bundle-report.html",
            gzipSize: true,
            open: false,
            template: "treemap",
          }),
        ]
      : []),
    ],
  resolve: {
    alias: {
      "@": path.resolve(import.meta.dirname, "./src"),
      "@ai-elements": path.resolve(import.meta.dirname, "./src/components/ai-elements"),
    },
  },
  server: {
    allowedHosts: true,
    proxy: {
      "/api": {
        target: process.env.VITE_API_TARGET ?? "http://127.0.0.1:5494",
        changeOrigin: true,
        ws: true, // Enable WebSocket proxy
      },
    },
  },
});
