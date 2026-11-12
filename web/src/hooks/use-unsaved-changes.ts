"use client";

import { useEffect } from "react";

const MESSAGE = "You have unsaved changes. Leave this page and lose them?";

/**
 * useUnsavedChangesWarning asks before the user leaves while `active` is
 * true. It covers the three ways out of a page:
 *
 *  - closing the tab, reloading, or typing a URL: the browser's own
 *    beforeunload prompt;
 *  - clicking any link inside the app: one capture-phase click listener;
 *  - the browser's Back button: a guard history entry.
 *
 * The App Router has no API for blocking a navigation, so each is handled at
 * the browser level.
 */
export function useUnsavedChangesWarning(active: boolean) {
  useEffect(() => {
    if (!active) return;

    const onBeforeUnload = (event: BeforeUnloadEvent) => {
      // Browsers show their own fixed wording; preventDefault is what asks.
      event.preventDefault();
    };

    // Capture phase on document runs before React's own listener on the app
    // root, so preventDefault here happens before next/link's click handler,
    // which skips navigation when the event is already default-prevented.
    const onClick = (event: MouseEvent) => {
      const link = (event.target as Element | null)?.closest?.("a[href]");
      if (!(link instanceof HTMLAnchorElement)) return;
      // A new tab, a download, a modified click or another site leaves this
      // page where it is (or is beforeunload's job); not ours to stop.
      if (link.target === "_blank" || link.hasAttribute("download")) return;
      if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
      if (link.origin !== window.location.origin) return;
      if (link.pathname === window.location.pathname && link.search === window.location.search) return;

      if (!window.confirm(MESSAGE)) {
        event.preventDefault();
        event.stopPropagation();
      }
    };

    // Back button: push a copy of the current entry, so Back first lands on
    // the same page. That popstate is the moment to ask. Staying pushes the
    // copy again; leaving goes back once more, for real. Next.js patches
    // pushState and copies its own history state into the new entry, so the
    // router treats the copy as one of its own.
    window.history.pushState(null, "", window.location.href);
    const onPopState = () => {
      if (window.confirm(MESSAGE)) {
        window.removeEventListener("popstate", onPopState);
        window.history.back();
      } else {
        window.history.pushState(null, "", window.location.href);
      }
    };

    window.addEventListener("beforeunload", onBeforeUnload);
    document.addEventListener("click", onClick, true);
    window.addEventListener("popstate", onPopState);
    return () => {
      window.removeEventListener("beforeunload", onBeforeUnload);
      document.removeEventListener("click", onClick, true);
      window.removeEventListener("popstate", onPopState);
    };
  }, [active]);
}
