import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  // Emits .next/standalone: a minimal server.js plus only the node_modules it
  // imports, so the Docker runtime image needs no `npm install`.
  output: "standalone",
  cacheComponents: true,
  partialPrefetching: true,
  experimental: {
    instantInsights: {
      // Validate only segments that export `instant`. The admin area renders
      // in the browser once it has read the session from localStorage, so
      // its pages have no server-rendered shell to validate, and implicit
      // validation of every page would report that on each one.
      validationLevel: "manual-warning",
    },
  },
  turbopack: {
    rules: {
      "*.css": {
        loaders: ["@tailwindcss/turbopack"],
        as: "*.css",
      },
    },
  },
};

export default nextConfig;
