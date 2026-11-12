"use client";

import {
  ArrowRightIcon,
  CircleXIcon,
  ExternalLinkIcon,
  FileTextIcon,
  GaugeIcon,
  InboxIcon,
  MessageSquareTextIcon,
  UsersIcon,
  type LucideIcon,
} from "lucide-react";
import Link from "next/link";
import { useParams } from "next/navigation";
import { useState } from "react";

import { ScoreBreakdown } from "@/components/score/score-breakdown";
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
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useApplication, useApplicationScore, useAssignments, useUpdateStatus } from "@/hooks/use-applications";
import { useJobPost } from "@/hooks/use-job-posts";
import { ApiError } from "@/lib/api/errors";
import type {
  ApplicationDetail as ApplicationDetailData,
  ApplicationStatus,
  Assignment,
  Question,
  Review,
} from "@/lib/api/types";
import { describeEvent, relativeTime, type TimelineLine } from "@/lib/applications/timeline";
import { formatDate } from "@/lib/format";

import { ApplicationStatusBadge } from "./cells";

/** ApplicationDetail is one candidate's application, laid out to support a decision about it. */
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
        <Skeleton className="h-64" />
        <Skeleton className="h-48" />
      </div>
    );
  }

  return <ApplicationBody application={app.data} />;
}

type TabValue = "answers" | "reviews" | "timeline";

/**
 * ApplicationBody renders a loaded application. A separate component so its
 * queries start only once the application, and so its role, is known.
 */
function ApplicationBody({ application: a }: { application: ApplicationDetailData }) {
  const score = useApplicationScore(a.id);
  const post = useJobPost(a.job_post_id);
  const assignments = useAssignments(a.id);
  const [tab, setTab] = useState<TabValue>("answers");

  // The detail endpoint carries no candidate name; the submission event's
  // actor is the candidate, resolved to a name when the timeline is read.
  const name = a.events.find((e) => e.type === "application.submitted")?.actor_name ?? "Candidate";
  const prompts = new Map((post.data?.questions ?? []).map((q) => [q.id, q]));
  const notScored = score.error instanceof ApiError && score.error.code === "not_scored";

  // Evidence links point into the Answers tab, which is unmounted while
  // another tab is open: switch first, then scroll once it has rendered.
  const showAnswer = (answerId: string) => {
    setTab("answers");
    requestAnimationFrame(() => {
      const el = document.getElementById(`answer-${answerId}`);
      el?.scrollIntoView({ block: "center" });
      el?.focus({ preventScroll: true });
    });
  };

  return (
    <div className="flex flex-col gap-6">
      <header className="flex flex-col gap-3">
        <Link href="/admin/applications" className="w-fit text-sm text-muted-foreground hover:underline">
          Applications
        </Link>
        <div className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
          <div className="flex min-w-0 flex-col gap-2">
            <div className="flex flex-wrap items-center gap-3">
              <h1 className="text-xl font-semibold tracking-tight">{name}</h1>
              <ApplicationStatusBadge status={a.status} />
            </div>
            <p className="flex flex-wrap items-center gap-x-3 gap-y-1 text-sm text-text-secondary">
              {post.data && (
                <Link href={`/admin/roles/${a.job_post_id}/ranking`} className="hover:underline">
                  {post.data.title}
                </Link>
              )}
              <span>Applied {formatDate(a.submitted_at)}</span>
              <a
                href={a.cv_url}
                target="_blank"
                rel="noreferrer noopener"
                className="inline-flex items-center gap-1 font-medium text-foreground hover:underline"
              >
                <FileTextIcon className="size-3.5" aria-hidden="true" />
                Open CV
                <ExternalLinkIcon className="size-3" aria-hidden="true" />
                <span className="sr-only">(opens in a new tab)</span>
              </a>
            </p>
          </div>
          <StatusActions id={a.id} name={name} allowed={a.allowed_next_statuses} />
        </div>
      </header>

      <div className="grid items-start gap-6 lg:grid-cols-[minmax(0,1fr)_20rem]">
        <section aria-labelledby="score-heading" className="overflow-hidden rounded-lg border bg-surface">
          <h2 id="score-heading" className="border-b px-4 py-3 font-medium">
            Score and evidence
          </h2>
          {notScored ? (
            <p className="p-4 text-sm text-text-secondary">
              Not scored: the role had no rubric when this application arrived. Save a rubric and rescore to score it.
            </p>
          ) : score.isError ? (
            <p className="p-4 text-sm text-destructive">{score.error.message}</p>
          ) : !score.data ? (
            <Skeleton className="m-4 h-48" />
          ) : (
            <ScoreBreakdown score={score.data} onShowAnswer={showAnswer} />
          )}
        </section>

        <aside className="flex flex-col gap-4">
          <ReviewSummary reviews={a.reviews} assignments={assignments.data} onOpen={() => setTab("reviews")} />
          <Interviewers assignments={assignments.data} loading={assignments.isPending} />
        </aside>
      </div>

      <Tabs value={tab} onValueChange={(v) => setTab(v as TabValue)} className="gap-4">
        <TabsList variant="line" className="w-full justify-start border-b">
          <TabsTrigger value="answers" className="flex-none">
            Answers <Count n={a.answers.length} />
          </TabsTrigger>
          <TabsTrigger value="reviews" className="flex-none">
            Reviews <Count n={a.reviews?.length ?? 0} />
          </TabsTrigger>
          <TabsTrigger value="timeline" className="flex-none">
            Timeline <Count n={a.events.length} />
          </TabsTrigger>
        </TabsList>
        <TabsContent value="answers">
          <Answers application={a} prompts={prompts} />
        </TabsContent>
        <TabsContent value="reviews">
          <Reviews reviews={a.reviews} />
        </TabsContent>
        <TabsContent value="timeline">
          <Timeline application={a} candidate={name} assignments={assignments.data} />
        </TabsContent>
      </Tabs>
    </div>
  );
}

function Count({ n }: { n: number }) {
  return <span className="font-mono text-xs text-muted-foreground tabular-nums">{n}</span>;
}

// The verb on each move's button. Keyed by the target status, so a move the
// server adds later still gets a button, labelled by its fallback.
const moveLabels: Partial<Record<ApplicationStatus, string>> = {
  shortlisted: "Shortlist",
  interview: "Move to interview",
  offer: "Make offer",
};

/**
 * StatusActions offers exactly the moves the server listed for this
 * application. It holds no copy of the pipeline: allowed comes from the
 * same Go table PATCH .../status enforces.
 */
function StatusActions({ id, name, allowed }: { id: string; name: string; allowed: ApplicationStatus[] }) {
  const update = useUpdateStatus(id);
  const [confirmReject, setConfirmReject] = useState(false);

  if (allowed.length === 0) {
    return <p className="text-sm text-muted-foreground">Final: no further moves.</p>;
  }

  const forward = allowed.filter((s) => s !== "rejected");
  return (
    <div className="flex flex-wrap gap-2">
      {forward.map((status) => (
        <Button key={status} onClick={() => update.mutate(status)} disabled={update.isPending}>
          {moveLabels[status] ?? `Move to ${status}`}
          <ArrowRightIcon aria-hidden="true" />
        </Button>
      ))}
      {allowed.includes("rejected") && (
        <>
          <Button variant="outline" onClick={() => setConfirmReject(true)} disabled={update.isPending}>
            <CircleXIcon aria-hidden="true" />
            Reject
          </Button>
          <AlertDialog open={confirmReject} onOpenChange={setConfirmReject}>
            <AlertDialogContent>
              <AlertDialogHeader>
                <AlertDialogTitle>Reject {name}?</AlertDialogTitle>
                <AlertDialogDescription>
                  Rejection is final: the application cannot be moved again afterwards.
                </AlertDialogDescription>
              </AlertDialogHeader>
              <AlertDialogFooter>
                <AlertDialogCancel>Cancel</AlertDialogCancel>
                <AlertDialogAction
                  variant="destructive"
                  onClick={() => {
                    setConfirmReject(false);
                    update.mutate("rejected");
                  }}
                >
                  Reject
                </AlertDialogAction>
              </AlertDialogFooter>
            </AlertDialogContent>
          </AlertDialog>
        </>
      )}
    </div>
  );
}

/** ReviewSummary is the interviewers' verdict in one glance: the mean mark and who is still to review. */
function ReviewSummary({
  reviews,
  assignments,
  onOpen,
}: {
  reviews: Review[] | undefined;
  assignments: Assignment[] | undefined;
  onOpen: () => void;
}) {
  const pending = (assignments ?? []).filter((x) => !x.reviewed);
  const mean = reviews && reviews.length > 0 ? reviews.reduce((sum, r) => sum + r.mark, 0) / reviews.length : null;

  return (
    <section aria-labelledby="verdict-heading" className="rounded-lg border bg-surface p-4">
      <h2 id="verdict-heading" className="mb-2 text-sm font-medium text-text-secondary">
        Interview reviews
      </h2>
      {mean === null ? (
        <p className="text-sm text-text-secondary">No reviews yet.</p>
      ) : (
        <p className="flex items-baseline gap-2">
          <span className="font-mono text-2xl font-semibold tabular-nums">{mean.toFixed(1)}</span>
          <span className="text-sm text-text-secondary">
            out of 10, mean of {reviews!.length} {reviews!.length === 1 ? "review" : "reviews"}
          </span>
        </p>
      )}
      {pending.length > 0 && (
        <p className="mt-1 text-sm text-text-secondary">
          Waiting on {pending.map((x) => x.interviewer_name).join(", ")}
        </p>
      )}
      {reviews && reviews.length > 0 && (
        <Button variant="link" size="sm" className="mt-1 h-auto px-0" onClick={onOpen}>
          Read the reviews
        </Button>
      )}
    </section>
  );
}

function Interviewers({ assignments, loading }: { assignments: Assignment[] | undefined; loading: boolean }) {
  return (
    <section aria-labelledby="interviewers-heading" className="rounded-lg border bg-surface p-4">
      <h2 id="interviewers-heading" className="mb-2 text-sm font-medium text-text-secondary">
        Interviewers
      </h2>
      {loading ? (
        <Skeleton className="h-10" />
      ) : !assignments || assignments.length === 0 ? (
        <p className="text-sm text-text-secondary">Nobody is assigned yet.</p>
      ) : (
        <ul className="flex flex-col gap-2">
          {assignments.map((x) => (
            <li key={x.interviewer_id} className="flex items-center justify-between gap-2 text-sm">
              <span className="min-w-0 truncate">{x.interviewer_name}</span>
              <span className={x.reviewed ? "text-xs text-foreground" : "text-xs text-muted-foreground"}>
                {x.reviewed ? "Reviewed" : "Not reviewed"}
              </span>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

function Answers({ application: a, prompts }: { application: ApplicationDetailData; prompts: Map<string, Question> }) {
  // In form order, not storage order, so the answers read as the candidate saw the form.
  const answers = [...a.answers].sort(
    (x, y) => (prompts.get(x.question_id)?.position ?? 0) - (prompts.get(y.question_id)?.position ?? 0),
  );
  return (
    <dl className="divide-y rounded-lg border bg-surface">
      {answers.map((ans) => {
        const q = prompts.get(ans.question_id);
        return (
          <div
            key={ans.id}
            id={`answer-${ans.id}`}
            // Focusable from script only, so an evidence link can move focus here.
            tabIndex={-1}
            className="flex scroll-mt-4 flex-col gap-1 px-4 py-3 outline-none focus:bg-primary-soft/40"
          >
            <dt className="text-sm text-muted-foreground">{q?.prompt ?? "Question"}</dt>
            <dd className="text-md break-words whitespace-pre-line">
              {/* A yes/no answer is stored as "true" or "false". */}
              {q?.kind === "boolean" ? (ans.answer === "true" ? "Yes" : "No") : ans.answer}
            </dd>
          </div>
        );
      })}
    </dl>
  );
}

function Reviews({ reviews }: { reviews: Review[] | undefined }) {
  // The server decides visibility (module 9.2): absent means this viewer may
  // see no reviews at all, which is different from none filed.
  if (reviews === undefined) {
    return <p className="text-text-secondary">Reviews are not visible to you.</p>;
  }
  if (reviews.length === 0) {
    return <p className="text-text-secondary">No interviewer has filed a review yet.</p>;
  }
  return (
    <ul className="flex flex-col gap-3">
      {reviews.map((r) => (
        <li key={r.id} className="rounded-lg border bg-surface p-4">
          <div className="flex items-baseline justify-between gap-3">
            <span className="font-medium">{r.interviewer_name}</span>
            <span className="font-mono tabular-nums">
              {r.mark} <span className="text-muted-foreground">/ 10</span>
            </span>
          </div>
          {r.notes ? (
            <p className="mt-2 break-words whitespace-pre-line text-text-secondary">{r.notes}</p>
          ) : (
            <p className="mt-2 text-muted-foreground">No notes.</p>
          )}
          <p className="mt-2 text-xs text-muted-foreground">
            {r.updated_at !== r.created_at ? "Edited " : "Filed "}
            <time dateTime={r.updated_at}>{formatDate(r.updated_at)}</time>
          </p>
        </li>
      ))}
    </ul>
  );
}

const lineIcons: Record<TimelineLine["kind"], LucideIcon> = {
  applied: InboxIcon,
  status: ArrowRightIcon,
  rejected: CircleXIcon,
  score: GaugeIcon,
  people: UsersIcon,
  review: MessageSquareTextIcon,
};

function Timeline({
  application: a,
  candidate,
  assignments,
}: {
  application: ApplicationDetailData;
  candidate: string;
  assignments: Assignment[] | undefined;
}) {
  // Interviewer IDs in payloads, resolved from what this page already holds.
  const names = new Map<string, string>();
  for (const x of assignments ?? []) names.set(x.interviewer_id, x.interviewer_name);
  for (const r of a.reviews ?? []) names.set(r.interviewer_id, r.interviewer_name);

  // Newest first: what changed most recently is what an admin returns for.
  const events = [...a.events].sort((x, y) => y.created_at.localeCompare(x.created_at));
  return (
    <ol className="flex flex-col">
      {events.map((e, i) => {
        const line = describeEvent(e, names, candidate);
        const Icon = lineIcons[line.kind];
        return (
          <li key={e.id} className="relative flex gap-3 pb-5 last:pb-0">
            {/* The connecting rule between icons; decorative. */}
            {i < events.length - 1 && <span aria-hidden="true" className="absolute top-7 bottom-0 left-3.5 w-px bg-border" />}
            <span className="flex size-7 shrink-0 items-center justify-center rounded-full border bg-surface text-muted-foreground">
              <Icon className="size-3.5" aria-hidden="true" />
            </span>
            <div className="flex min-w-0 flex-col pt-0.5">
              <p>{line.sentence}</p>
              <time dateTime={e.created_at} title={formatDate(e.created_at)} className="text-xs text-muted-foreground">
                {relativeTime(e.created_at)}
              </time>
            </div>
          </li>
        );
      })}
    </ol>
  );
}
