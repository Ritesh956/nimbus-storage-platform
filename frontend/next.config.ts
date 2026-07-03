import path from "path";
import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  // Pins the workspace root to this directory — without it, Turbopack was
  // inferring the root from a stray lockfile in a parent directory outside
  // this project and warning on every build.
  turbopack: {
    root: path.join(__dirname),
  },
};

export default nextConfig;
