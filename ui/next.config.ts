import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  // Static export: Wails wraps the built assets. There is no Next server in
  // production — the daemon is the only backend.
  output: "export",
  images: { unoptimized: true },
};

export default nextConfig;
