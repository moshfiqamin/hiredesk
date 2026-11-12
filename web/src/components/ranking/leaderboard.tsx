"use client";

import { ChevronRightIcon, TrophyIcon } from "lucide-react";
import Link from "next/link";
import { useParams } from "next/navigation";
import { Fragment } from "react";
import { cn } from "cn";

import { ApplicationStatusBadge, band } from "@/components/applications/cells";
import { EmptyState, ErrorState } from "@/components/states/states";
import { Skeleton } from "@/components/ui/skeleton";
import { useRanking } from "@/hooks/use-applications";
import { useJobPost } from "@/hooks/use-job-posts";
import type { RankingEntry } from "@/lib/api/types";

// Segment fill per band, the same patterns as the score breakdown.
const fill = {
  strong: "bg-band-strong",
  adequate:
    "bg-[repeating-linear-gradient(135deg,var(--band-adequate)_0_2px,color-mix(in_oklch,var(--band-adequate),transparent_55%)_2px_4px)]",
  weak: "bg-band-weak",
} as const;

/**
 * Leaderboard is every applicant to one role, best score first, with a line
 * where the vacancies run out. Rank, score and status are facts in text; the
 * mini bar only helps the eye compare rows.
 */
export function Leaderboard() {
  const { id } = useParams<{ id: string }>();
  const ranking = useRanking(id);
  const post = useJobPost(id);

  if (ranking.isError) {
    return <ErrorState error={ranking.error} title="Could not load the ranking" onRetry={() => void ranking.refetch()} />;
  }
  if (!ranking.data) {
    return (
      <div className="flex flex-col gap-2" aria-busy="true" aria-label="Loading the ranking">
        {Array.from({ length: 6 }, (_, i) => (
          <Skeleton key={i} className="h-14" />
        ))}
      </div>
    );
  }

  const rows = ranking.data.data;
  if (rows.length === 0) {
    return (
      <EmptyState icon={TrophyIcon} title="No applicants yet">
        The ranking fills in as applications arrive and are scored against the rubric.
      </EmptyState>
    );
  }

  const vacancies = post.data?.vacancies ?? 0;
  // The line is drawn only if it falls inside the list and above unscored rows:
  // an unscored applicant has no place relative to a score cut-off.
  const scoredCount = rows.filter((r) => r.percentage !== null).length;
  const cutoff = vacancies > 0 && vacancies < scoredCount ? vacancies : null;

  return (
    <div className="flex flex-col gap-3">
      <p className="text-sm text-text-secondary">
        {rows.length} {rows.length === 1 ? "applicant" : "applicants"}, best total score first
        {vacancies > 0 && (
          <>
            {" "}
            · {vacancies} {vacancies === 1 ? "vacancy" : "vacancies"}
          </>
        )}
      </p>
      <ol aria-label="Ranking" className="divide-y overflow-hidden rounded-lg border bg-surface">
        {rows.map((row, i) => (
          <Fragment key={row.application_id}>
            {cutoff !== null && i === cutoff && <CutoffLine vacancies={vacancies} />}
            <Row row={row} withinVacancies={cutoff !== null ? i < cutoff : vacancies > 0 && row.percentage !== null && i < vacancies} />
          </Fragment>
        ))}
      </ol>
    </div>
  );
}

function CutoffLine({ vacancies }: { vacancies: number }) {
  return (
    // aria-hidden on the rule only; the words are read, so a screen-reader
    // user meets the cut-off at the same place in the list.
    <li className="flex items-center gap-3 border-y-2 border-dashed border-primary/50 bg-surface-sunken px-4 py-1.5 text-xs font-medium text-primary-ink">
      Cut-off: the {vacancies === 1 ? "top applicant fills the vacancy" : `top ${vacancies} fill the vacancies`}
    </li>
  );
}

function Row({ row, withinVacancies }: { row: RankingEntry; withinVacancies: boolean }) {
  return (
    <li className="relative">
      <Link
        href={`/admin/applications/${row.application_id}`}
        className="grid grid-cols-[2.25rem_minmax(0,1fr)_auto] items-center gap-x-3 gap-y-1 px-4 py-3 hover:bg-surface-sunken focus-visible:bg-surface-sunken focus-visible:outline-none sm:grid-cols-[2.25rem_minmax(0,1fr)_8rem_13rem_1rem]"
      >
        <span
          className={cn(
            "flex size-8 items-center justify-center rounded-md font-mono text-sm tabular-nums",
            // Above the line: a quiet tint, not a medal. The rank stays a number.
            withinVacancies ? "bg-primary-soft font-semibold text-primary-ink" : "text-muted-foreground",
          )}
        >
          {row.rank}
          {withinVacancies && <span className="sr-only">, within the vacancies</span>}
        </span>
        <span className="min-w-0 truncate font-medium">{row.candidate_name}</span>
        <span className="hidden sm:block">
          <ApplicationStatusBadge status={row.status} />
        </span>
        <Score row={row} />
        <ChevronRightIcon className="hidden size-4 text-muted-foreground sm:block" aria-hidden="true" />
        {/* On a phone the status moves under the name, keeping three columns. */}
        <span className="col-start-2 sm:hidden">
          <ApplicationStatusBadge status={row.status} />
        </span>
      </Link>
    </li>
  );
}

/**
 * Score is the total as a number, then a bar split by criterion: each
 * segment as wide as that criterion's points available, filled as far as
 * was earned. The bar starts at zero and its full width is the whole rubric.
 */
function Score({ row }: { row: RankingEntry }) {
  if (row.percentage === null || row.total_available === null) {
    return <span className="row-span-2 text-right text-sm text-muted-foreground sm:row-span-1">Not scored</span>;
  }
  const total = row.total_available;
  const summary = row.breakdown.map((c) => `${c.criterion} ${c.earned} of ${c.available}`).join(", ");

  return (
    // On a phone the bar stacks under the number, across both grid rows.
    <span className="row-span-2 flex flex-col items-end gap-1.5 sm:row-span-1 sm:flex-row sm:items-center sm:justify-end sm:gap-3">
      <span className="w-14 text-right font-mono text-sm font-semibold tabular-nums">{row.percentage.toFixed(1)}%</span>
      <span aria-hidden="true" title={summary} className="flex h-2 w-24 gap-0.5 sm:h-2.5 sm:w-36">
        {row.breakdown.map((c, i) => (
          <span key={i} className="relative h-full rounded-[2px] bg-surface-sunken" style={{ width: `${(c.available / total) * 100}%` }}>
            <span
              className={cn("absolute inset-y-0 left-0 rounded-[2px]", fill[band(c.available > 0 ? (c.earned / c.available) * 100 : 0)])}
              style={{ width: `${c.available > 0 ? (c.earned / c.available) * 100 : 0}%` }}
            />
          </span>
        ))}
      </span>
      <span className="sr-only">: {summary}</span>
    </span>
  );
}
