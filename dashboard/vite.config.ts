import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

// The build output goes where the Go binary embeds it (internal/webui/dist).
export default defineConfig({
  plugins: [react()],
  build: {
    outDir: "../internal/webui/dist",
    emptyOutDir: false,
  },
  server: {
    proxy: { "/api": "http://localhost:8080" },
  },
});
