"use client";

import { LockIcon, RocketIcon } from "lucide-react";
import Link from "next/link";
import { useParams, usePathname } from "next/navigation";
import { useState, type ReactNode } from "react";

import { ErrorState } from "@/components/states/states";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { useJobPost, useTransitionJobPost } from "@/hooks/use-job-posts";
import type { JobPost } from "@/lib/api/types";

import { StatusBadge } from "./status-badge";

/** The sections of one role, each its own URL so it can be linked to. */
const tabs = [
  { segment: "", label: "Overview" },
  { segment: "/edit", label: "Details" },
  { segment: "/questions", label: "Questions" },
] as const;

/**
 * RoleShell is the frame of every page about one role: breadcrumb, title,
 * status, the lifecycle action and the section tabs.
 */
export function RoleShell({ children }: { children: ReactNode }) {
  const { id } = useParams<{ id: string }>();
  const pathname = usePathname();
  const post = useJobPost(id);

  // A missing role (or a bad ID) fails every section the same way, so the
  // error is shown once here rather than by each page.
  if (post.isError && !post.data) {
    return <ErrorState error={post.error} title="Could not load this role" onRetry={() => void post.refetch()} />;
  }

  const base = `/admin/roles/${id}`;

  return (
    <div className="flex flex-col gap-6">
      <header className="flex flex-col gap-4">
        <Link href="/admin/roles" className="w-fit text-sm text-muted-foreground hover:underline">
          Roles
        </Link>
        <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
          {post.data ? (
            <div className="flex min-w-0 flex-wrap items-center gap-3">
              <h1 className="text-xl font-semibold tracking-tight">{post.data.title}</h1>
              <StatusBadge status={post.data.status} />
            </div>
          ) : (
            <Skeleton className="h-8 w-72" />
          )}
          {post.data && <LifecycleAction post={post.data} />}
        </div>
        <nav aria-label="Role sections" className="-mb-px flex gap-1 overflow-x-auto border-b">
          {tabs.map((tab) => {
            const href = base + tab.segment;
            const active = pathname === href;
            return (
              <Link
                key={tab.segment}
                href={href}
                aria-current={active ? "page" : undefined}
                className="border-b-2 border-transparent px-3 py-2 text-sm whitespace-nowrap text-text-secondary hover:text-foreground aria-[current=page]:border-primary aria-[current=page]:font-medium aria-[current=page]:text-foreground"
              >
                {tab.label}
              </Link>
            );
          })}
        </nav>
      </header>
      {children}
    </div>
  );
}

/** LifecycleAction is the one next move for this role: open a draft, or close an open role. */
function LifecycleAction({ post }: { post: JobPost }) {
  const transition = useTransitionJobPost();
  const [confirmClose, setConfirmClose] = useState(false);

  if (post.status === "draft") {
    return (
      <Button
        variant="outline"
        onClick={() => transition.mutate({ id: post.id, to: "open", title: post.title })}
        disabled={transition.isPending}
      >
        <RocketIcon aria-hidden="true" />
        Open for applications
      </Button>
    );
  }

  if (post.status === "open") {
    return (
      <>
        <Button variant="outline" onClick={() => setConfirmClose(true)} disabled={transition.isPending}>
          <LockIcon aria-hidden="true" />
          Close role
        </Button>
        <AlertDialog open={confirmClose} onOpenChange={setConfirmClose}>
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle>Close “{post.title}”?</AlertDialogTitle>
              <AlertDialogDescription>
                Candidates will no longer be able to apply. Closing is final: a closed role cannot be reopened.
              </AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel>Keep it open</AlertDialogCancel>
              <AlertDialogAction
                variant="destructive"
                onClick={() => {
                  setConfirmClose(false);
                  transition.mutate({ id: post.id, to: "closed", title: post.title });
                }}
              >
                Close role
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
      </>
    );
  }

  return null;
}
