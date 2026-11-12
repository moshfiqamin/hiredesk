"use client";

import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { use, useEffect, type ReactNode } from "react";
import { browser } from "react-dom";

import { buttonVariants } from "@/components/ui/button";
import type { Role } from "@/lib/api/types";
import { useSession } from "@/lib/auth/session";
import { homeFor } from "@/lib/auth/home";

/**
 * RoleGate renders its children only for a signed-in user with the given
 * role. Nobody signed in is sent to the login page, with this page as the
 * destination; the wrong role gets a short explanation and a link home.
 *
 * It is a convenience, not security: every request is authorised by the
 * API, which refuses the wrong role whatever this component renders.
 */
export function RoleGate({ role, children }: { role: Role; children: ReactNode }) {
  // The session lives in localStorage, so this subtree renders in the
  // browser only; on the server it suspends to the layout's fallback.
  use(browser("The session is kept in localStorage."));

  const session = useSession();
  const router = useRouter();
  const pathname = usePathname();

  useEffect(() => {
    if (!session) router.replace(`/login?next=${encodeURIComponent(pathname)}`);
  }, [session, router, pathname]);

  if (!session) return null;

  if (session.user.role !== role) {
    return (
      <div className="mx-auto flex max-w-md flex-col gap-3 py-16">
        <h1 className="text-xl font-semibold">This page is for {role}s</h1>
        <p className="text-text-secondary">
          You are signed in as {session.user.full_name}, an {session.user.role}.
        </p>
        <div>
          <Link href={homeFor(session.user.role)} className={buttonVariants({ variant: "outline" })}>
            Go to your home page
          </Link>
        </div>
      </div>
    );
  }

  return children;
}
