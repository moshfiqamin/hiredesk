"use client";


import { CircleAlertIcon } from "lucide-react";
import { cn } from "cn";

import { band } from "@/components/applications/cells";
import type { ScoreBreakdown as ScoreBreakdownData, ScoreEntry } from "@/lib/api/types";
import { highlight, matchedKeywords, zeroReason } from "@/lib/score/highlight";

// Fill per band: solid, hatched, dashed outline. The pattern repeats the
// band for anyone who cannot tell the hues apart; the number is always shown.
const fill = {
  strong: "bg-band-strong",
  adequate:
    "bg-[repeating-linear-gradient(135deg,var(--band-adequate)_0_2px,color-mix(in_oklch,var(--band-adequate),transparent_55%)_2px_4px)]",
  weak: "border border-dashed border-band-weak bg-band-weak-bg",
} as const;

const fmt = (n: number) => (Number.isInteger(n) ? String(n) : n.toFixed(1));

/**
 * ScoreBreakdown is the score of one application: the total, then one bar
 * per criterion, heaviest first, each with the evidence behind it.
 *
 * Every bar shares one points axis starting at zero: the track is as long as
 * the criterion's available points, the fill as long as what was earned. So
 * the bars compare weight as well as share, and "24 of 30" is visible.
 */
export function ScoreBreakdown({
  score,
  onShowAnswer,
}: {
  score: ScoreBreakdownData;
  /** Called by "Read the full answer" instead of following the link, when the answer is in a closed tab. */
  onShowAnswer?: (answerId: string) => void;
}) {
  // Copied before sorting: sort mutates, and score is query-cache data.
  const entries = [...score.breakdown].sort((a, b) => b.available - a.available);
  const maxAvailable = Math.max(...entries.map((e) => e.available), 1);
  const totalBand = band(score.percentage);

  return (
    <div className="flex flex-col">
      <div className="flex flex-wrap items-end justify-between gap-3 border-b px-4 py-4">
        <p className="flex items-baseline gap-3">
          <span className="font-mono text-4xl font-semibold tracking-tight tabular-nums">
            {score.percentage.toFixed(1)}%
          </span>
          <span className="text-sm text-text-secondary">
            {fmt(score.total_earned)} of {fmt(score.total_available)} points
          </span>
          <span className="score-band capitalize" data-band={totalBand}>
            {totalBand}
          </span>
        </p>
        <p className="text-xs text-muted-foreground">Rubric v{score.rubric_version}</p>
      </div>
      <ol className="divide-y">
        {entries.map((entry, i) => (
          <CriterionRow key={`${entry.criterion}-${i}`} entry={entry} maxAvailable={maxAvailable} onShowAnswer={onShowAnswer} />
        ))}
      </ol>
    </div>
  );
}

function CriterionRow({
  entry,
  maxAvailable,
  onShowAnswer,
}: {
  entry: ScoreEntry;
  maxAvailable: number;
  onShowAnswer?: (answerId: string) => void;
}) {
  const share = entry.available > 0 ? (entry.earned / entry.available) * 100 : 0;
  const b = band(share);
  const why = zeroReason(entry.earned, entry.detail);
  const keywords = entry.kind === "keyword_match" ? matchedKeywords(entry.detail) : [];

  return (
    <li className="flex flex-col gap-2 px-4 py-4">
      <div className="flex items-baseline justify-between gap-3">
        <span className="font-medium">{entry.criterion}</span>
        <span className="font-mono text-sm whitespace-nowrap tabular-nums">
          {fmt(entry.earned)} <span className="text-muted-foreground">of {fmt(entry.available)}</span>
        </span>
      </div>

      {/* The numbers above are the accessible fact; the bar repeats them visually. */}
      <span
        aria-hidden="true"
        className="relative block h-3 rounded-sm bg-surface-sunken"
        style={{ width: `${(entry.available / maxAvailable) * 100}%` }}
      >
        {/* A zero gets no fill at all, not a zero-width one whose border would
            still show: the empty track is the message. */}
        {share > 0 && (
          <span
            className={cn("absolute inset-y-0 left-0 rounded-sm", fill[b], "motion-safe:animate-in motion-safe:slide-in-from-left-2")}
            style={{ width: `${share}%` }}
          />
        )}
      </span>

      {why ? (
        <p className="flex items-start gap-1.5 text-sm font-medium text-band-weak">
          <CircleAlertIcon className="mt-0.5 size-4 shrink-0" aria-hidden="true" />
          {why}
        </p>
      ) : (
        <p className="text-sm text-text-secondary">{entry.detail}</p>
      )}

      {entry.evidence && (
        <figure className="flex flex-col gap-1 border-l-2 pl-3">
          <figcaption className="text-xs text-muted-foreground">{entry.evidence.question}</figcaption>
          <blockquote className="text-sm break-words">
            {/* Each segment is a text child, so React escapes it: answer text can never become markup. */}
            {highlight(entry.evidence.excerpt, keywords).map((s, i) =>
              s.match ? (
                <mark key={i} className="rounded-sm bg-primary-soft px-0.5 font-medium text-primary-ink">
                  {s.text}
                </mark>
              ) : (
                <span key={i}>{s.text}</span>
              ),
            )}
          </blockquote>
          <a
            href={`#answer-${entry.evidence.answer_id}`}
            onClick={(e) => {
              if (!onShowAnswer || !entry.evidence) return;
              e.preventDefault();
              onShowAnswer(entry.evidence.answer_id);
            }}
            className="w-fit text-xs text-muted-foreground hover:underline"
          >
            Read the full answer
          </a>
        </figure>
      )}
    </li>
  );
}
