// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import react from "@vitejs/plugin-react";
import { fileURLToPath, URL } from "node:url";
import { defineConfig } from "vite";

export default defineConfig({
  plugins: [react()],
  build: {
    outDir: "dist/client",
    sourcemap: process.env.VITE_BUILD_SOURCEMAP === "true",
  },
  resolve: {
    alias: {
      "@": fileURLToPath(new URL("./src", import.meta.url)),
    },
  },
  server: {
    port: 5173,
  },
});
