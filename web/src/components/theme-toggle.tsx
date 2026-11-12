"use client";

import { MoonIcon, SunIcon } from "lucide-react";
import { useTheme } from "next-themes";

import { Button } from "@/components/ui/button";

/**
 * ThemeToggle switches between light and dark. It flips the theme that is
 * actually showing (resolvedTheme), so a visitor on "system" sees the switch
 * work on the first click. resolvedTheme is read only inside the click
 * handler, never during render, so the markup is identical on server and
 * client.
 */
export function ThemeToggle() {
  const { resolvedTheme, setTheme } = useTheme();

  return (
    // The label is constant on purpose. The server cannot know the theme, so a
    // label computed from it would differ between server and client HTML, and
    // React does not repair attribute mismatches after hydration.
    <Button
      variant="ghost"
      size="icon"
      aria-label="Toggle colour theme"
      onClick={() => setTheme(resolvedTheme === "dark" ? "light" : "dark")}
    >
      {/* Both icons render and CSS picks one, so the right icon shows before
          React hydrates and nothing flickers. */}
      <SunIcon className="hidden dark:block" aria-hidden="true" />
      <MoonIcon className="block dark:hidden" aria-hidden="true" />
    </Button>
  );
}
