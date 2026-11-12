"use client";

import { useSyncExternalStore } from "react";

import type { TokenPair, User } from "@/lib/api/types";

/**
 * Session is what the browser keeps after a login: both tokens and the user
 * they belong to.
 */
export interface Session {
  accessToken: string;
  refreshToken: string;
  user: User;
}

const STORAGE_KEY = "hiredesk.session";

// undefined means "not read from storage yet", which is different from null,
// "read, and nobody is signed in".
let current: Session | null | undefined;
const listeners = new Set<() => void>();

function read(): Session | null {
  try {
    const raw = window.localStorage.getItem(STORAGE_KEY);
    return raw ? (JSON.parse(raw) as Session) : null;
  } catch {
    // Storage can be blocked (private mode, a strict browser setting) or hold
    // something unparseable; either way nobody is signed in.
    return null;
  }
}

function notify() {
  for (const listener of listeners) listener();
}

/** getSession returns the signed-in session, or null. Browser only. */
export function getSession(): Session | null {
  if (current === undefined) current = read();
  return current;
}

/** setSession stores a session, or clears it with null, and tells every subscriber. */
export function setSession(next: Session | null) {
  current = next;
  try {
    if (next) window.localStorage.setItem(STORAGE_KEY, JSON.stringify(next));
    else window.localStorage.removeItem(STORAGE_KEY);
  } catch {
    // The in-memory copy still works for this tab.
  }
  notify();
}

/** sessionFromTokens converts a login or refresh response into a Session. */
export function sessionFromTokens(tokens: TokenPair): Session {
  return { accessToken: tokens.access_token, refreshToken: tokens.refresh_token, user: tokens.user };
}

function subscribe(listener: () => void) {
  listeners.add(listener);

  // Another tab signing in or out writes localStorage, which fires "storage"
  // here (never in the tab that wrote it). Re-reading keeps every tab on the
  // same session.
  const onStorage = (event: StorageEvent) => {
    if (event.key !== STORAGE_KEY) return;
    current = read();
    notify();
  };
  window.addEventListener("storage", onStorage);

  return () => {
    listeners.delete(listener);
    window.removeEventListener("storage", onStorage);
  };
}

/**
 * useSession returns the current session and re-renders when it changes,
 * in this tab or another. useSyncExternalStore is React's hook for reading a
 * store that lives outside React; the third argument is what the server
 * renders, where there is no localStorage.
 */
export function useSession(): Session | null {
  return useSyncExternalStore(subscribe, getSession, () => null);
}
