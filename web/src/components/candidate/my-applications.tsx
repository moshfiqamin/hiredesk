"use client";

import { useQueries, useQuery } from "@tanstack/react-query";
import { CheckIcon, InboxIcon, XIcon } from "lucide-react";
import Link from "next/link";
import { cn } from "cn";

import { EmptyState, ErrorState } from "@/components/states/states";
import { buttonVariants } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { api } from "@/lib/api/client";
import type { ApplicationDetail, ApplicationListItem, ApplicationPage, ApplicationStatus } from "@/lib/api/types";
import { formatDate } from "@/lib/format";
import { queryKeys } from "@/lib/query/keys";

// The forward pipeline, in order. "rejected" is not a step: it can happen
// after any of these, and ends the track there.
const steps: { status: Exclude<ApplicationStatus, "rejected">; label: string }[] = [
  { status: "submitted", label: "Received" },
  { status: "shortlisted", label: "Shortlisted" },
  { status: "interview", label: "Interview" },
  { status: "offer", label: "Offer" },
];

/**
 * MyApplications is the candidate's own applications, each with where it is
 * in the pipeline.
 *
 * No score is shown, deliberately; the server refuses a candidate's score
 * request with a 403 in any case (docs/decisions.md D3). See
 * docs/notes/14-2 for the reasoning.
 */
export function MyApplications() {
  const list = useQuery({
    // The candidate's list has no filters; the shared key shape still
    // applies, so a submit's invalidation of every list refreshes this one.
    queryKey: queryKeys.applications.list({ mine: "candidate" }),
    queryFn: ({ signal }) => api<ApplicationPage>("/v1/applications", { signal, query: { limit: 100 } }),
  });

  // For a rejected application the list says only "rejected", not when. Its
  // own detail holds the status events, so it is fetched for those alone:
  // a candidate has a handful of applications, not hundreds.
  const rejected = (list.data?.data ?? []).filter((a) => a.status === "rejected");
  const details = useQueries({
    queries: rejected.map((a) => ({
      queryKey: queryKeys.applications.detail(a.id),
      queryFn: ({ signal }: { signal: AbortSignal }) => api<ApplicationDetail>(`/v1/applications/${a.id}`, { signal }),
    })),
  });
  const stoppedAt = new Map<string, ApplicationStatus>();
  details.forEach((d, i) => {
    const event = d.data?.events.find((e) => e.type === "application.status_changed" && e.payload.to === "rejected");
    if (event) stoppedAt.set(rejected[i].id, String(event.payload.from) as ApplicationStatus);
  });

  return (
    <div className="flex flex-col gap-6">
      <header className="flex flex-col gap-2">
        <h1 className="text-2xl font-semibold tracking-tight">My applications</h1>
        <p className="max-w-prose text-text-secondary">
          Every application is read by a person on the hiring team. You will see each step here as it happens.
        </p>
      </header>

      {list.isError ? (
        <ErrorState error={list.error} title="Could not load your applications" onRetry={() => void list.refetch()} />
      ) : !list.data ? (
        <div className="flex flex-col gap-3" aria-busy="true" aria-label="Loading your applications">
          <Skeleton className="h-36" />
          <Skeleton className="h-36" />
        </div>
      ) : list.data.data.length === 0 ? (
        <EmptyState
          icon={InboxIcon}
          title="You have not applied for anything yet"
          action={
            <Link href="/roles" className={buttonVariants()}>
              Browse open roles
            </Link>
          }
        >
          When you apply, the application and its progress appear here.
        </EmptyState>
      ) : (
        <ul className="flex flex-col gap-4">
          {list.data.data.map((a) => (
            <li key={a.id}>
              <ApplicationCard application={a} stoppedAt={stoppedAt.get(a.id)} />
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

function ApplicationCard({ application: a, stoppedAt }: { application: ApplicationListItem; stoppedAt?: ApplicationStatus }) {
  const rejected = a.status === "rejected";
  return (
    <article className="flex flex-col gap-5 rounded-xl border bg-surface p-5">
      <div className="flex flex-col gap-1 sm:flex-row sm:items-baseline sm:justify-between">
        <h2 className="text-lg font-semibold tracking-tight">
          <Link href={`/roles/${a.job_post_id}`} className="rounded-sm outline-none hover:underline focus-visible:ring-2 focus-visible:ring-ring">
            {a.job_post_title}
          </Link>
        </h2>
        <p className="text-sm text-text-secondary">
          Submitted <time dateTime={a.submitted_at}>{formatDate(a.submitted_at)}</time>
        </p>
      </div>
      <Tracker status={a.status} stoppedAt={stoppedAt} />
      {rejected && (
        <p className="text-sm text-text-secondary">
          This application is not going further. Thank you for applying; you are welcome to apply for other roles.
        </p>
      )}
    </article>
  );
}

/**
 * Tracker shows the pipeline as numbered steps. Each step says in text
 * whether it is done, current or still to come (for screen readers, and for
 * anyone who cannot tell the fills apart), and the current one carries
 * aria-current="step".
 */
function Tracker({ status, stoppedAt }: { status: ApplicationStatus; stoppedAt?: ApplicationStatus }) {
  const rejected = status === "rejected";
  // How far the application got: its status, or for a rejection the stage it
  // was rejected from (submitted if the detail has not loaded yet).
  const reached = steps.findIndex((s) => s.status === (rejected ? (stoppedAt ?? "submitted") : status));

  return (
    <ol aria-label="Progress" className="grid grid-cols-4 gap-2">
      {steps.map((step, i) => {
        const done = i < reached || (!rejected && i === reached && status === "offer");
        const current = !rejected && i === reached && status !== "offer";
        const stopped = rejected && i === reached;
        const state = stopped ? "not progressing" : done ? "done" : current ? "current step" : "to come";
        return (
          <li key={step.status} aria-current={current ? "step" : undefined} className="flex flex-col gap-2">
            {/* The bar: solid for done, primary outline for current, a cross
                where a rejection stopped it, empty for steps to come. */}
            <span
              aria-hidden="true"
              className={cn(
                "flex h-1.5 rounded-full",
                // Reached is solid, current included: the application is
                // there. The word "Now" below marks which one is current.
                (done || current) && "bg-primary",
                stopped && "bg-band-weak",
                !done && !current && !stopped && "bg-surface-sunken",
              )}
            />
            <span className="flex items-center gap-1 text-xs sm:text-sm">
              {done && <CheckIcon className="size-3.5 text-primary-ink" aria-hidden="true" />}
              {stopped && <XIcon className="size-3.5 text-band-weak" aria-hidden="true" />}
              <span className={cn(current || done ? "font-medium text-foreground" : "text-muted-foreground", stopped && "text-band-weak")}>
                {step.label}
              </span>
              {current && (
                <span aria-hidden="true" className="rounded-sm bg-primary-soft px-1 text-[0.625rem] font-medium tracking-wide text-primary-ink uppercase">
                  Now
                </span>
              )}
              <span className="sr-only">: {state}</span>
            </span>
          </li>
        );
      })}
    </ol>
  );
}
