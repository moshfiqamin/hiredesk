"use client";

import { QueryClientProvider } from "@tanstack/react-query";
import dynamic from "next/dynamic";
import { Suspense, useState, type ReactNode } from "react";

import { makeQueryClient } from "@/lib/query/client";

// Loaded only in development, and only in the browser. process.env.NODE_ENV
// is replaced with a literal at build time, so in a production build this is
// `false ? … : …` and the bundler drops the import entirely.
const ReactQueryDevtools =
  process.env.NODE_ENV === "development"
    ? dynamic(() => import("@tanstack/react-query-devtools").then((m) => m.ReactQueryDevtools), { ssr: false })
    : () => null;

/**
 * Providers holds the client-side state shared by every page: the TanStack
 * Query cache.
 */
export function Providers({ children }: { children: ReactNode }) {
  // useState with an initialiser creates the client once per mounted tree. A
  // module-level client would be shared by every request the server renders,
  // leaking one user's cached data into another's page.
  const [queryClient] = useState(makeQueryClient);

  return (
    <QueryClientProvider client={queryClient}>
      {children}
      {/* The devtools render in the browser only. Their own boundary keeps
          that from holding back the rest of the page. */}
      <Suspense fallback={null}>
        <ReactQueryDevtools buttonPosition="bottom-left" />
      </Suspense>
    </QueryClientProvider>
  );
}
