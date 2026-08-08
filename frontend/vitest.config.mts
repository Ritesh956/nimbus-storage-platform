import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";
import tsconfigPaths from "vite-tsconfig-paths";

export default defineConfig({
  plugins: [tsconfigPaths(), react()],
  test: {
    environment: "jsdom",
    setupFiles: ["./vitest.setup.ts"],
    exclude: ["node_modules", ".next", "e2e/**"],
    // Default "forks" pool times out spawning worker child processes on
    // this machine — this repo's path contains spaces ("Ritesh Gupta",
    // "FINAL PROJECTS"), and the pool's worker bootstrap mis-escapes it
    // (visible as a %20-mangled relative path in the failure). "threads"
    // uses worker_threads instead of child_process, which doesn't go
    // through the same path-as-CLI-arg step and isn't affected.
    pool: "threads",
    coverage: {
      provider: "v8",
      exclude: [
        "**/*.config.*",
        "**/*.test.*",
        "lib/api-schema.generated.ts", // generated, see that file's own header
        ".next/**",
      ],
    },
  },
});
