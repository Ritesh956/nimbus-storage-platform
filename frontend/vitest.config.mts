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
      // Audit §14's "Improve" note calls for a CI coverage threshold once
      // the numbers stabilize — `npm run test:coverage` currently can't run
      // at all to produce one. Every worker (threads, forks, vmThreads;
      // isolate:true/false; fileParallelism:true/false; maxWorkers:1) times
      // out its startup handshake at exactly 60s the instant `--coverage`
      // is added, reproduced identically on Windows, Alpine (musl), and
      // Debian (glibc) with ample CPU/RAM — not a path-with-spaces or
      // resource-starvation issue like the pool footgun above. Matches a
      // known, still-open upstream bug class in Vitest 4's pool
      // implementation (vitest-dev/vitest#8766, #8968, #8861, #9494) at
      // this exact vitest@4.1.10. `npm test` (no coverage) is unaffected —
      // CI intentionally does not run test:coverage until vitest ships a
      // fix; don't add a coverage step/threshold without re-verifying this
      // first, or CI will hang/fail on every push.
    },
  },
});
