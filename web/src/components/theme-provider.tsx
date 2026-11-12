"use client";

import { ThemeProvider as NextThemesProvider } from "next-themes";
import type { ComponentProps } from "react";

/**
 * ThemeProvider wraps next-themes so the root layout, a Server Component, can
 * render it. next-themes reads localStorage and matchMedia, which exist only
 * in the browser, so the wrapper has to be a Client Component.
 */
export function ThemeProvider(props: ComponentProps<typeof NextThemesProvider>) {
  return <NextThemesProvider {...props} />;
}
