"use client";

import { ArrowRightIcon, CheckCircle2Icon, ClipboardListIcon, CircleDotIcon } from "lucide-react";
import Link from "next/link";
import { cn } from "cn";

import { ApplicationStatusBadge, ScoreCell } from "@/components/applications/cells";
import { EmptyState, ErrorState } from "@/components/states/states";
import { buttonVariants } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { useMyAssignments } from "@/hooks/use-reviews";
import type { ApplicationListItem } from "@/lib/api/types";
import { formatDate } from "@/lib/format";

/**
 * byWorkFirst orders the worklist: not yet reviewed before reviewed, and
 * within each, the application waiting longest first. An interviewer opens
 * this page to clear what is waiting on them, so that is the top of it.
 */
function byWorkFirst(a: ApplicationListItem, b: ApplicationListItem): number {
  const ra = a.reviewed_by_me ? 1 : 0;
  const rb = b.reviewed_by_me ? 1 : 0;
  if (ra !== rb) return ra - rb;
  return a.submitted_at.localeCompare(b.submitted_at);
}

/** AssignmentsView is an interviewer's worklist: every application they are assigned to review. */
export function AssignmentsView() {
  const list = useMyAssignments();
  const rows = [...(list.data?.data ?? [])].sort(byWorkFirst);
  const toReview = rows.filter((r) => !r.reviewed_by_me).length;

  return (
    <div className="flex flex-col gap-6">
      <header className="flex flex-col gap-2">
        <h1 className="text-2xl font-semibold tracking-tight">My assignments</h1>
        {list.data && (
          <p className="text-text-secondary" role="status">
            {toReview === 0 ? "You are all caught up." : `${toReview} waiting for your review`}
            {rows.length - toReview > 0 && ` · ${rows.length - toReview} reviewed`}
          </p>
        )}
      </header>

      {list.isError ? (
        <ErrorState error={list.error} title="Could not load your assignments" onRetry={() => void list.refetch()} />
      ) : !list.data ? (
        <div className="flex flex-col gap-2" aria-busy="true" aria-label="Loading your assignments">
          {Array.from({ length: 4 }, (_, i) => (
            <Skeleton key={i} className="h-20" />
          ))}
        </div>
      ) : rows.length === 0 ? (
        <EmptyState icon={ClipboardListIcon} title="Nothing assigned to you yet">
          When an admin assigns you a candidate to interview, they appear here.
        </EmptyState>
      ) : (
        <ul className="flex flex-col divide-y overflow-hidden rounded-xl border bg-surface">
          {rows.map((row) => (
            <AssignmentRow key={row.id} row={row} />
          ))}
        </ul>
      )}
    </div>
  );
}

function AssignmentRow({ row }: { row: ApplicationListItem }) {
  const reviewed = Boolean(row.reviewed_by_me);
  return (
    <li className="grid gap-3 px-4 py-4 sm:grid-cols-[minmax(0,1fr)_auto_auto] sm:items-center sm:gap-6">
      <div className="flex min-w-0 flex-col gap-1">
        <p className="flex flex-wrap items-center gap-x-3 gap-y-1">
          <span className="font-medium">{row.candidate_name}</span>
          <ApplicationStatusBadge status={row.status} />
        </p>
        <p className="truncate text-sm text-text-secondary">
          {row.job_post_title} · applied {formatDate(row.submitted_at)}
        </p>
      </div>
      <div className="flex items-center justify-between gap-4 sm:justify-end">
        <ScoreCell percentage={row.score?.percentage} />
        <span className={cn("inline-flex items-center gap-1.5 text-sm whitespace-nowrap", reviewed ? "text-text-secondary" : "font-medium text-foreground")}>
          {reviewed ? (
            <CheckCircle2Icon className="size-4 text-primary-ink" aria-hidden="true" />
          ) : (
            <CircleDotIcon className="size-4 text-band-adequate" aria-hidden="true" />
          )}
          {reviewed ? "Reviewed" : "To review"}
        </span>
      </div>
      <Link
        href={`/assignments/${row.id}`}
        className={buttonVariants({ variant: reviewed ? "outline" : "default", className: "w-full sm:w-auto" })}
      >
        {reviewed ? "View your review" : "Review"}
        <span className="sr-only"> {row.candidate_name}</span>
        <ArrowRightIcon aria-hidden="true" />
      </Link>
    </li>
  );
}
