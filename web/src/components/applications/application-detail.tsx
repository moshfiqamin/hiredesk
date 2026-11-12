"use client";

import { ExternalLinkIcon } from "lucide-react";
import Link from "next/link";
import { useParams } from "next/navigation";

import { ErrorState } from "@/components/states/states";
import { Skeleton } from "@/components/ui/skeleton";
import { useApplication, useApplicationScore } from "@/hooks/use-applications";
import { useJobPost } from "@/hooks/use-job-posts";
import { ApiError } from "@/lib/api/errors";
import type { ApplicationDetail as ApplicationDetailData, ApplicationEvent } from "@/lib/api/types";
import { formatDate } from "@/lib/format";

import { ApplicationStatusBadge, ScoreCell, band } from "./cells";

const eventLabels: Record<string, string> = {
  "application.submitted": "Applied",
  "application.status_changed": "Status changed",
  "application.rescored": "Rescored",
  "application.assigned": "Interviewer assigned",
  "application.unassigned": "Interviewer unassigned",
  "application.reviewed": "Reviewed",
  "application.review_updated": "Review updated",
};

function describe(e: ApplicationEvent): string {
  const p = e.payload;
  if (e.type === "application.status_changed" && p.from && p.to) return `${String(p.from)} → ${String(p.to)}`;
  return "";
}

/** ApplicationDetail is one candidate's application: score breakdown, answers and history. */
export function ApplicationDetail() {
  const { id } = useParams<{ id: string }>();
  const app = useApplication(id);

  if (app.isError) {
    return <ErrorState error={app.error} title="Could not load this application" onRetry={() => void app.refetch()} />;
  }
  if (!app.data) {
    return (
      <div className="flex flex-col gap-4" aria-busy="true" aria-label="Loading the application">
        <Skeleton className="h-10 w-80" />
        <Skeleton className="h-48" />
        <Skeleton className="h-64" />
      </div>
    );
  }

  return <ApplicationBody application={app.data} />;
}

/**
 * ApplicationBody renders a loaded application. A separate component so its
 * queries start only once the application, and so its role, is known.
 */
function ApplicationBody({ application: a }: { application: ApplicationDetailData }) {
  const score = useApplicationScore(a.id);
  const post = useJobPost(a.job_post_id);

  // The detail endpoint carries no candidate name. The submission event's
  // actor is the candidate, and the timeline resolves actors' names when it
  // is read (module 9.3), so that is where the name comes from.
  const name = a.events.find((e) => e.type === "application.submitted")?.actor_name ?? "Candidate";
  const prompts = new Map((post.data?.questions ?? []).map((q) => [q.id, q]));
  const answers = [...a.answers].sort(
    (x, y) => (prompts.get(x.question_id)?.position ?? 0) - (prompts.get(y.question_id)?.position ?? 0),
  );
  const notScored = score.error instanceof ApiError && score.error.code === "not_scored";

  return (
    <div className="flex flex-col gap-6">
      <header className="flex flex-col gap-3">
        <Link href="/admin/applications" className="w-fit text-sm text-muted-foreground hover:underline">
          Applications
        </Link>
        <div className="flex flex-wrap items-center gap-3">
          <h1 className="text-xl font-semibold tracking-tight">{name}</h1>
          <ApplicationStatusBadge status={a.status} />
        </div>
        <p className="flex flex-wrap items-center gap-x-3 gap-y-1 text-sm text-text-secondary">
          {post.data && (
            <Link href={`/admin/roles/${a.job_post_id}`} className="hover:underline">
              {post.data.title}
            </Link>
          )}
          <span>Applied {formatDate(a.submitted_at)}</span>
          <a href={a.cv_url} target="_blank" rel="noreferrer noopener" className="inline-flex items-center gap-1 hover:underline">
            CV <ExternalLinkIcon className="size-3.5" aria-hidden="true" />
          </a>
        </p>
      </header>

      <div className="grid items-start gap-6 lg:grid-cols-[1fr_22rem]">
        <div className="flex flex-col gap-6">
          <section aria-labelledby="score-heading" className="rounded-lg border bg-surface">
            <header className="flex items-center justify-between gap-3 border-b px-4 py-3">
              <h2 id="score-heading" className="font-medium">
                Score
              </h2>
              {score.data && (
                <span className="flex items-center gap-3">
                  <ScoreCell percentage={score.data.percentage} />
                  <span className="score-band capitalize" data-band={band(score.data.percentage)}>
                    {band(score.data.percentage)}
                  </span>
                </span>
              )}
            </header>
            {notScored ? (
              <p className="p-4 text-sm text-text-secondary">
                Not scored: the role had no rubric when this application arrived. Save a rubric and rescore to score it.
              </p>
            ) : score.isError ? (
              <p className="p-4 text-sm text-destructive">{score.error.message}</p>
            ) : !score.data ? (
              <Skeleton className="m-4 h-24" />
            ) : (
              <ol className="divide-y">
                {score.data.breakdown.map((entry, i) => {
                  const share = entry.available > 0 ? (entry.earned / entry.available) * 100 : 0;
                  return (
                    <li key={i} className="flex flex-col gap-1.5 px-4 py-3">
                      <div className="flex items-baseline justify-between gap-3">
                        <span className="font-medium">{entry.criterion}</span>
                        <span className="font-mono text-sm tabular-nums">
                          {entry.earned} <span className="text-muted-foreground">/ {entry.available}</span>
                        </span>
                      </div>
                      <span aria-hidden="true" className="h-1.5 overflow-hidden rounded-full bg-surface-sunken">
                        <span className="block h-full rounded-full bg-primary" style={{ width: `${share}%` }} />
                      </span>
                      <p className="text-sm text-text-secondary">{entry.detail}</p>
                      {entry.evidence && (
                        <blockquote className="border-l-2 pl-3 text-sm text-text-secondary">
                          <span className="block text-xs text-muted-foreground">{entry.evidence.question}</span>…
                          {entry.evidence.excerpt}…
                        </blockquote>
                      )}
                    </li>
                  );
                })}
              </ol>
            )}
          </section>

          <section aria-labelledby="answers-heading" className="rounded-lg border bg-surface">
            <h2 id="answers-heading" className="border-b px-4 py-3 font-medium">
              Answers
            </h2>
            <dl className="divide-y">
              {answers.map((ans) => (
                <div key={ans.id} className="flex flex-col gap-1 px-4 py-3">
                  <dt className="text-sm text-muted-foreground">{prompts.get(ans.question_id)?.prompt ?? "Question"}</dt>
                  <dd className="text-md whitespace-pre-line">
                    {/* A yes/no answer is stored as "true" or "false". */}
                    {prompts.get(ans.question_id)?.kind === "boolean" ? (ans.answer === "true" ? "Yes" : "No") : ans.answer}
                  </dd>
                </div>
              ))}
            </dl>
          </section>
        </div>

        <section aria-labelledby="history-heading" className="rounded-lg border bg-surface">
          <h2 id="history-heading" className="border-b px-4 py-3 font-medium">
            History
          </h2>
          <ol className="flex flex-col gap-3 p-4">
            {a.events.map((e) => (
              <li key={e.id} className="text-sm">
                <p className="font-medium">
                  {eventLabels[e.type] ?? e.type}
                  {describe(e) && <span className="font-normal text-text-secondary"> · {describe(e)}</span>}
                </p>
                <p className="text-xs text-muted-foreground">
                  {e.actor_name ?? "System"} · <time dateTime={e.created_at}>{formatDate(e.created_at)}</time>
                </p>
              </li>
            ))}
          </ol>
        </section>
      </div>
    </div>
  );
}
