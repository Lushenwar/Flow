import { defineConfig } from "vitest/config";
import path from "node:path";

export default defineConfig({
  test: {
    // jsdom only for the component tests; the pure state tests do not need it,
    // but one environment is simpler than a per-file override.
    environment: "jsdom",
    setupFiles: ["./vitest.setup.ts"],
  },
  resolve: {
    alias: { "@": path.resolve(__dirname, "src") },
  },
});
