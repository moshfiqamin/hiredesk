import type { Metadata } from "next";
import localFont from "next/font/local";

import { Providers } from "@/components/providers";
import { ThemeProvider } from "@/components/theme-provider";
import { Toaster } from "@/components/ui/sonner";

import "./globals.css";

// Fonts are self-hosted from the @fontsource npm packages (SIL Open Font
// Licence) rather than fetched from Google Fonts at build time, so a build —
// including the Docker image — needs no network access beyond npm.
const instrumentSans = localFont({
  src: "../../node_modules/@fontsource-variable/instrument-sans/files/instrument-sans-latin-wght-normal.woff2",
  variable: "--font-instrument-sans",
  weight: "400 700",
  display: "swap",
});

const plexMono = localFont({
  src: [
    {
      path: "../../node_modules/@fontsource/ibm-plex-mono/files/ibm-plex-mono-latin-400-normal.woff2",
      weight: "400",
    },
    {
      path: "../../node_modules/@fontsource/ibm-plex-mono/files/ibm-plex-mono-latin-500-normal.woff2",
      weight: "500",
    },
  ],
  variable: "--font-plex-mono",
  display: "swap",
});

export const metadata: Metadata = {
  title: {
    default: "HireDesk",
    template: "%s · HireDesk",
  },
  description: "Review, score and rank job applications against a versioned rubric.",
};

export default function RootLayout({ children }: LayoutProps<"/">) {
  return (
    // next-themes sets class="dark" on <html> before React hydrates, so the
    // server and client class lists differ by design. suppressHydrationWarning
    // silences that one expected mismatch, on this element only.
    <html
      lang="en"
      suppressHydrationWarning
      className={`${instrumentSans.variable} ${plexMono.variable} h-full`}
    >
      <body className="min-h-full">
        <ThemeProvider
          attribute="class"
          defaultTheme="system"
          enableSystem
          disableTransitionOnChange
        >
          <Providers>{children}</Providers>
          <Toaster />
        </ThemeProvider>
      </body>
    </html>
  );
}
