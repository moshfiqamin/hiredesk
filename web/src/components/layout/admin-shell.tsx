"use client";

import { useQueryClient } from "@tanstack/react-query";
import { BriefcaseIcon, InboxIcon, LogOutIcon, MenuIcon } from "lucide-react";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { use, useEffect, useState, type ReactNode } from "react";
import { browser } from "react-dom";

import { ThemeToggle } from "@/components/theme-toggle";
import { Button } from "@/components/ui/button";
import { Sheet, SheetContent, SheetTitle, SheetTrigger } from "@/components/ui/sheet";
import { api } from "@/lib/api/client";
import { getSession, setSession, useSession } from "@/lib/auth/session";

const nav = [
  { href: "/admin/roles", label: "Roles", icon: BriefcaseIcon },
  { href: "/admin/applications", label: "Applications", icon: InboxIcon },
] as const;

/**
 * AdminShell is the frame around every admin page: navigation, the signed-in
 * user, and the gate that sends anyone else to the login page.
 *
 * The session lives in localStorage, which the server cannot read. use(browser())
 * renders this subtree in the browser only; on the server it suspends to the
 * layout's Suspense fallback. So the first render here already knows who is
 * signed in, and never flashes the wrong state.
 */
export function AdminShell({ children }: { children: ReactNode }) {
  use(browser("The session is kept in localStorage."));

  const session = useSession();
  const router = useRouter();
  const pathname = usePathname();
  const queryClient = useQueryClient();
  const [drawerOpen, setDrawerOpen] = useState(false);

  useEffect(() => {
    if (!session) router.replace(`/login?next=${encodeURIComponent(pathname)}`);
  }, [session, router, pathname]);

  async function signOut() {
    const refreshToken = getSession()?.refreshToken;
    setSession(null);
    // Cached data belongs to the user who fetched it; the next one starts empty.
    queryClient.clear();
    if (refreshToken) {
      // Revokes the refresh token on the server. Best effort: the session is
      // already gone from this browser whatever the answer.
      await api("/v1/auth/logout", { method: "POST", body: { refresh_token: refreshToken }, auth: false }).catch(() => {});
    }
  }

  if (!session) return null;

  if (session.user.role !== "admin") {
    return (
      <main className="mx-auto flex min-h-dvh max-w-md flex-col justify-center gap-3 px-4">
        <h1 className="text-xl font-semibold">This area is for admins</h1>
        <p className="text-text-secondary">
          You are signed in as {session.user.full_name}, an {session.user.role}. Sign in with an admin
          account to manage roles and applications.
        </p>
        <div>
          <Button variant="outline" onClick={signOut}>
            Sign out
          </Button>
        </div>
      </main>
    );
  }

  const navLinks = (onNavigate?: () => void) => (
    <nav aria-label="Main" className="flex flex-col gap-1">
      {nav.map(({ href, label, icon: Icon }) => {
        const active = pathname === href || pathname.startsWith(href + "/");
        return (
          <Link
            key={href}
            href={href}
            onClick={onNavigate}
            // aria-current tells assistive technology which link is the
            // current page; the styling below keys off the same attribute.
            aria-current={active ? "page" : undefined}
            className="flex items-center gap-2 rounded-md px-2 py-2 text-sm text-sidebar-foreground outline-none hover:bg-sidebar-accent hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring aria-[current=page]:bg-surface-selected aria-[current=page]:font-medium aria-[current=page]:text-primary-ink md:py-1.5"
          >
            <Icon className="size-4" aria-hidden="true" />
            {label}
          </Link>
        );
      })}
    </nav>
  );

  const account = (
    <div className="flex flex-col gap-2 px-2">
      <div className="text-sm">
        <div className="truncate font-medium">{session.user.full_name}</div>
        {/* text-secondary, not muted: muted-foreground on the sidebar's
            darker surface measured 4.4:1, under the 4.5:1 AA minimum. */}
        <div className="truncate text-xs text-text-secondary">{session.user.email}</div>
      </div>
      <div className="flex items-center gap-1">
        <ThemeToggle />
        <Button variant="ghost" size="sm" onClick={signOut}>
          <LogOutIcon aria-hidden="true" />
          Sign out
        </Button>
      </div>
    </div>
  );

  return (
    <div className="flex min-h-dvh flex-col md:flex-row">
      {/* Below md: a slim top bar, with the navigation in a drawer. */}
      <header className="sticky top-0 z-40 flex items-center justify-between border-b bg-sidebar px-3 py-2 md:hidden">
        <Link href="/admin/roles" className="rounded-sm px-1 font-semibold tracking-tight outline-none focus-visible:ring-2 focus-visible:ring-ring">
          HireDesk
        </Link>
        <Sheet open={drawerOpen} onOpenChange={setDrawerOpen}>
          <SheetTrigger render={<Button variant="ghost" size="icon" aria-label="Open menu" />}>
            <MenuIcon aria-hidden="true" />
          </SheetTrigger>
          <SheetContent>
            <SheetTitle className="px-2 pt-1">HireDesk</SheetTitle>
            {/* Closing on navigate: a drawer left open over the new page
                would hide what the person just asked to see. */}
            {navLinks(() => setDrawerOpen(false))}
            <div className="mt-auto">{account}</div>
          </SheetContent>
        </Sheet>
      </header>

      {/* From md: the sidebar. */}
      <aside className="hidden w-56 shrink-0 flex-col gap-1 border-r bg-sidebar px-3 py-5 md:flex">
        <Link href="/admin/roles" className="mb-6 rounded-sm px-2 font-semibold tracking-tight outline-none focus-visible:ring-2 focus-visible:ring-ring">
          HireDesk
        </Link>
        {navLinks()}
        <div className="mt-auto pt-6">{account}</div>
      </aside>
      <main id="main" className="min-w-0 flex-1 px-4 py-6 sm:px-6 lg:px-8">
        {children}
      </main>
    </div>
  );
}
