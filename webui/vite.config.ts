import path from "node:path"
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import tailwindcss from '@tailwindcss/vite'

// https://vite.dev/config/
export default defineConfig({
  plugins: [vue(), tailwindcss()],
  resolve: {
    alias: {
      "@": path.resolve(import.meta.dirname, "./src"),
    },
  },
  base: "./",
  build: {
    // Built assets are embedded directly into the central service binary
    // (internal/centralserver/ui.go, docs/05-recipe-and-crd.md §5.3) — no
    // separate frontend service to deploy.
    outDir: path.resolve(import.meta.dirname, "../internal/centralserver/ui"),
    emptyOutDir: true,
  },
})
