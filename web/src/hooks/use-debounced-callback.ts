"use client";

import { useCallback, useEffect, useRef } from "react";

/**
 * useDebouncedCallback returns a function that runs `fn` only once calls to
 * it have stopped for `delay` milliseconds.
 *
 * Each call clears the pending timer and starts a new one, so a burst of
 * calls (one per keystroke) collapses into a single run, `delay` after the
 * last. The timer is cleared on unmount, so a run never lands on a page the
 * user has already left.
 */
export function useDebouncedCallback<A extends unknown[]>(fn: (...args: A) => void, delay: number) {
  // The latest fn, so the timer calls the current closure (with the current
  // router and URL) rather than the one from the render that started it.
  const latest = useRef(fn);
  useEffect(() => {
    latest.current = fn;
  });

  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  useEffect(() => () => clearTimeout(timer.current), []);

  // useCallback keeps the returned function the same across renders, so it
  // can be passed down without causing re-renders.
  return useCallback(
    (...args: A) => {
      clearTimeout(timer.current);
      timer.current = setTimeout(() => latest.current(...args), delay);
    },
    [delay],
  );
}
