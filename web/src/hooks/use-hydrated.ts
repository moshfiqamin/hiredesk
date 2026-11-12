"use client";

import { useSyncExternalStore } from "react";

const noop = () => () => {};

/**
 * useHydrated is false on the server and during hydration, true afterwards.
 * A page that renders on the server but shows something only the browser
 * knows (the session, in localStorage) waits for it, so the server's HTML
 * and the first client render match and React has nothing to repair.
 */
export function useHydrated(): boolean {
  return useSyncExternalStore(
    noop,
    () => true,
    () => false,
  );
}
