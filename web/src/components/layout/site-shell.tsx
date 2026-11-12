"use client";

import { useQueryClient } from "@tanstack/react-query";
import { LogOutIcon, MenuIcon } from "lucide-react";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { Suspense, useState, type ReactNode } from "react";

import { ThemeToggle } from "@/components/theme-toggle";
import { Button, buttonVariants } from "@/components/ui/button";
import { useHydrated } from "@/hooks/use-hydrated";
import { Sheet, SheetContent, SheetTitle, SheetTrigger } from "@/components/ui/sheet";
import { api } from "@/lib/api/client";
import type { Role } from "@/lib/api/types";
import { getSession, setSession, useSession } from "@/lib/auth/session";

// The links each kind of visitor gets. Everyone sees the open roles; a
// signed-in user also gets the page for what their role came to do.
const linksFor = (role: Role | null) => [
  { href: "/roles", label: "Open roles" },
  ...(role === "candidate" ? [{ href: "/my-applications", label: "My applications" }] : []),
  ...(role === "interviewer" ? [{ href: "/assignments", label: "My assignments" }] : []),
  ...(role === "admin" ? [{ href: "/admin/roles", label: "Admin" }] : []),
];

/**
 * SiteShell frames every page outside /admin: the public role pages, the
 * candidate's applications and the interviewer's assignments.
 *
 * The header reads the URL (to mark the current link), which this version
 * of Next.js only knows at request time; inside <Suspense> it streams in
 * after the prerendered page, with a static header holding its place.
 */
export function SiteShell({ children }: { children: ReactNode }) {
  return (
    <div className="flex min-h-dvh flex-col">
      <Suspense fallback={<HeaderFrame />}>
        <SiteHeader />
      </Suspense>
      <main id="main" className="mx-auto w-full max-w-6xl flex-1 px-4 py-8 sm:px-6">
        {children}
      </main>
    </div>
  );
}

/** HeaderFrame is the header's box and logo, shared by the fallback and the real header. */
function HeaderFrame({ children }: { children?: ReactNode }) {
  return (
    <header className="sticky top-0 z-40 border-b bg-background/90 backdrop-blur supports-[backdrop-filter]:bg-background/75">
      <div className="mx-auto flex h-14 max-w-6xl items-center justify-between gap-3 px-4 sm:px-6">
        <Link
          href="/roles"
          className="rounded-sm font-semibold tracking-tight outline-none focus-visible:ring-2 focus-visible:ring-ring"
        >
          HireDesk
        </Link>
        {children}
      </div>
    </header>
  );
}

function SiteHeader() {
  const hydrated = useHydrated();
  const session = useSession();
  const role = hydrated ? (session?.user.role ?? null) : null;
  const pathname = usePathname();
  const router = useRouter();
  const queryClient = useQueryClient();
  const [open, setOpen] = useState(false);

  async function signOut() {
    const refreshToken = getSession()?.refreshToken;
    setSession(null);
    queryClient.clear();
    router.push("/roles");
    if (refreshToken) {
      // Best effort: the session is already gone from this browser.
      await api("/v1/auth/logout", { method: "POST", body: { refresh_token: refreshToken }, auth: false }).catch(
        () => {},
      );
    }
  }

  const links = linksFor(role);
  const linkList = (onNavigate?: () => void, vertical = false) =>
    links.map(({ href, label }) => (
      <Link
        key={href}
        href={href}
        onClick={onNavigate}
        aria-current={pathname === href || pathname.startsWith(href + "/") ? "page" : undefined}
        className={
          "rounded-md px-2.5 py-1.5 text-sm text-text-secondary outline-none hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring aria-[current=page]:font-medium aria-[current=page]:text-foreground " +
          (vertical ? "py-2" : "")
        }
      >
        {label}
      </Link>
    ));

  const accountActions = (onNavigate?: () => void) =>
    !hydrated ? null : session ? (
      <Button variant="ghost" size="sm" onClick={() => void signOut()}>
        <LogOutIcon aria-hidden="true" />
        Sign out
      </Button>
    ) : (
      <>
        <Link
          href={`/login?next=${encodeURIComponent(pathname)}`}
          onClick={onNavigate}
          className={buttonVariants({ variant: "ghost", size: "sm" })}
        >
          Sign in
        </Link>
        <Link href="/register" onClick={onNavigate} className={buttonVariants({ size: "sm" })}>
          Create account
        </Link>
      </>
    );

  return (
    <HeaderFrame>
      <nav aria-label="Main" className="hidden items-center gap-1 md:flex">
        {linkList()}
      </nav>
      <div className="hidden items-center gap-1 md:flex">
        <ThemeToggle />
        {accountActions()}
      </div>
      <Sheet open={open} onOpenChange={setOpen}>
        <SheetTrigger render={<Button variant="ghost" size="icon" className="md:hidden" aria-label="Open menu" />}>
          <MenuIcon aria-hidden="true" />
        </SheetTrigger>
        <SheetContent side="right">
          <SheetTitle className="px-2 pt-1">HireDesk</SheetTitle>
          <nav aria-label="Main" className="flex flex-col gap-1">
            {linkList(() => setOpen(false), true)}
          </nav>
          <div className="mt-auto flex flex-wrap items-center gap-2">
            <ThemeToggle />
            {accountActions(() => setOpen(false))}
          </div>
        </SheetContent>
      </Sheet>
    </HeaderFrame>
  );
}
