"use client";

import { ArrowLeftIcon, ExternalLinkIcon, FileTextIcon } from "lucide-react";
import Link from "next/link";
import { useParams } from "next/navigation";

import { Answers } from "@/components/applications/application-detail";
import { ApplicationStatusBadge } from "@/components/applications/cells";
import { ScoreBreakdown } from "@/components/score/score-breakdown";
import { ErrorState } from "@/components/states/states";
import { Skeleton } from "@/components/ui/skeleton";
import { useApplication, useApplicationScore } from "@/hooks/use-applications";
import { useJobPost } from "@/hooks/use-job-posts";
import { ApiError } from "@/lib/api/errors";
import { getSession } from "@/lib/auth/session";
import { formatDate } from "@/lib/format";

import { ReviewPanel } from "./review-form";

/**
 * InterviewerApplication is one assigned application as its interviewer sees
 * it: the answers and CV to judge, the engine's score for context, and their
 * own review. A 404 means not assigned, or not there: the API does not say
 * which (docs/decisions.md D1).
 */
export function InterviewerApplication() {
  const { id } = useParams<{ id: string }>();
  const app = useApplication(id);
  const score = useApplicationScore(id);
  const post = useJobPost(app.data?.job_post_id ?? "", { enabled: Boolean(app.data) });

  if (app.isError) {
    const notYours = app.error instanceof ApiError && app.error.status === 404;
    return notYours ? (
      <div className="flex flex-col gap-3 py-8">
        <h1 className="text-xl font-semibold">This application is not assigned to you</h1>
        <Link href="/assignments" className="w-fit text-sm font-medium underline-offset-2 hover:underline">
          Back to your assignments
        </Link>
      </div>
    ) : (
      <ErrorState error={app.error} title="Could not load this application" onRetry={() => void app.refetch()} />
    );
  }
  if (!app.data) {
    return (
      <div className="flex flex-col gap-4" aria-busy="true" aria-label="Loading the application">
        <Skeleton className="h-10 w-72" />
        <Skeleton className="h-96" />
      </div>
    );
  }

  const a = app.data;
  const me = getSession()?.user.id;
  // The server sends an interviewer their own review at most; the filter is
  // only to be explicit about whose review this panel edits.
  const mine = a.reviews?.find((r) => r.interviewer_id === me);
  const name = a.events.find((e) => e.type === "application.submitted")?.actor_name ?? "Candidate";
  const prompts = new Map((post.data?.questions ?? []).map((q) => [q.id, q]));

  return (
    <div className="flex flex-col gap-6">
      <header className="flex flex-col gap-3">
        <Link href="/assignments" className="inline-flex w-fit items-center gap-1 rounded-sm text-sm text-muted-foreground outline-none hover:underline focus-visible:ring-2 focus-visible:ring-ring">
          <ArrowLeftIcon className="size-3.5" aria-hidden="true" />
          My assignments
        </Link>
        <div className="flex flex-wrap items-center gap-3">
          <h1 className="text-2xl font-semibold tracking-tight">{name}</h1>
          <ApplicationStatusBadge status={a.status} />
        </div>
        <p className="flex flex-wrap items-center gap-x-3 gap-y-1 text-sm text-text-secondary">
          {post.data && <span>{post.data.title}</span>}
          <span>Applied {formatDate(a.submitted_at)}</span>
          <a href={a.cv_url} target="_blank" rel="noreferrer noopener" className="inline-flex items-center gap-1 font-medium text-foreground hover:underline">
            <FileTextIcon className="size-3.5" aria-hidden="true" />
            Open CV
            <ExternalLinkIcon className="size-3" aria-hidden="true" />
            <span className="sr-only">(opens in a new tab)</span>
          </a>
        </p>
      </header>

      <div className="grid items-start gap-6 lg:grid-cols-[minmax(0,1fr)_24rem]">
        {/* The review first on a phone: it is what the interviewer is here
            to do. Beside the evidence from lg up. */}
        <div className="lg:order-2 lg:sticky lg:top-20">
          <ReviewPanel applicationId={a.id} review={mine} />
        </div>

        <div className="flex min-w-0 flex-col gap-6 lg:order-1">
          <section aria-labelledby="answers-heading" className="flex flex-col gap-3">
            <h2 id="answers-heading" className="text-lg font-semibold">
              Answers
            </h2>
            <Answers application={a} prompts={prompts} />
          </section>
          <section aria-labelledby="score-heading" className="overflow-hidden rounded-lg border bg-surface">
            <h2 id="score-heading" className="border-b px-4 py-3 font-medium">
              Screening score
            </h2>
            {score.isError ? (
              <p className="p-4 text-sm text-text-secondary">
                {score.error instanceof ApiError && score.error.code === "not_scored" ? "Not scored yet." : score.error.message}
              </p>
            ) : !score.data ? (
              <Skeleton className="m-4 h-40" />
            ) : (
              <ScoreBreakdown score={score.data} />
            )}
          </section>
        </div>
      </div>
    </div>
  );
}
