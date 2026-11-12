import type { Role } from "@/lib/api/types";

/** homeFor is where each role lands after signing in: the page for what they came to do. */
export function homeFor(role: Role): string {
  switch (role) {
    case "admin":
      return "/admin/roles";
    case "interviewer":
      return "/assignments";
    case "candidate":
      return "/my-applications";
  }
}

/**
 * safeNext returns the ?next= path from the current URL if it is a path on
 * this site, else null. "//evil.example" is a protocol-relative URL to
 * another host, so a leading "//" is refused too: an open redirect would let
 * a phishing link bounce through the sign-in page.
 */
export function safeNext(): string | null {
  const next = new URLSearchParams(window.location.search).get("next");
  return next && next.startsWith("/") && !next.startsWith("//") ? next : null;
}
