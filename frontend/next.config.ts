import path from "path";
import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  // Pins the workspace root to this directory — without it, Turbopack was
  // inferring the root from a stray lockfile in a parent directory outside
  // this project and warning on every build.
  turbopack: {
    root: path.join(__dirname),
  },
  // Minimal, self-contained server bundle for the Dockerfile.web build
  // (Day 13) — copies only the files `next start` actually needs into
  // .next/standalone instead of shipping node_modules wholesale.
  output: "standalone",
};

export default nextConfig;
