import { AlertTriangleIcon, CheckCircle2Icon, CircleDashedIcon } from "lucide-react";
import type { ReactNode } from "react";

import { REQUIRED_TOTAL, toHundredths, type CriterionDraft } from "@/lib/rubric/schema";

// Segment fills cycle through tints of the one accent. Segments are told
// apart by their number, which the legend and each card repeat, so the
// shades only need to separate neighbours, not to carry meaning.
export const tones = [
  "bg-primary text-primary-foreground",
  "bg-primary/70 text-primary-foreground",
  "bg-primary/45 text-foreground",
  "bg-primary/25 text-foreground",
];

/** formatPoints renders hundredths as points: 1250 → "12.5". */
export function formatPoints(hundredths: number): string {
  return String(Math.round(hundredths) / 100);
}

export type TotalState = "under" | "exact" | "over";

/**
 * weightTotal sums the weights, derived on every render from the form's
 * values rather than kept in state of its own.
 */
export function weightTotal(criteria: CriterionDraft[]) {
  const parts = criteria.map((c, i) => ({
    number: i + 1,
    label: c.label.trim() || `Criterion ${i + 1}`,
    hundredths: toHundredths(c.weight) ?? 0,
  }));
  const total = parts.reduce((sum, p) => sum + p.hundredths, 0);
  const target = REQUIRED_TOTAL * 100;
  const state: TotalState = total < target ? "under" : total === target ? "exact" : "over";
  return { parts, total, target, state, difference: Math.abs(target - total) };
}

/**
 * WeightPanel shows the running total, its state, the shape of the rubric as
 * one proportional bar, and the save action with the reason it is or is not
 * available.
 */
export function WeightPanel({ criteria, action }: { criteria: CriterionDraft[]; action: ReactNode }) {
  const { parts, total, target, state, difference } = weightTotal(criteria);
  // The bar's full width is 100 points, or the total when it is over, so an
  // over-allocated rubric visibly overflows the 100 mark instead of being
  // squeezed to fit.
  const scale = Math.max(target, total);
  const pct = (h: number) => `${(h / scale) * 100}%`;

  const status = {
    under: {
      icon: CircleDashedIcon,
      tone: "text-text-secondary",
      title: `${formatPoints(difference)} ${difference === 100 ? "point" : "points"} left to allocate`,
      body: "Weights must add up to exactly 100 before the rubric can be saved. Raise a weight, or add a criterion.",
    },
    exact: {
      icon: CheckCircle2Icon,
      tone: "text-primary-ink",
      title: "Weights add up to 100",
      body: "Each criterion's weight is the most points it can award.",
    },
    over: {
      icon: AlertTriangleIcon,
      tone: "text-destructive",
      title: `${formatPoints(difference)} ${difference === 100 ? "point" : "points"} over 100`,
      body: "Weights must add up to exactly 100 before the rubric can be saved. Lower a weight, or remove a criterion.",
    },
  }[state];
  const Icon = status.icon;

  return (
    <section aria-labelledby="weights-heading" className="flex flex-col gap-4 rounded-lg border bg-surface p-4">
      <div className="flex items-end justify-between gap-3">
        <h2 id="weights-heading" className="text-sm font-medium text-muted-foreground">
          Total weight
        </h2>
        <p className="font-mono leading-none tabular-nums" aria-label={`${formatPoints(total)} of 100`}>
          <span className={`text-2xl font-medium ${state === "over" ? "text-destructive" : ""}`}>{formatPoints(total)}</span>
          <span className="text-md text-muted-foreground"> / 100</span>
        </p>
      </div>

      <div>
        {/* The bar is a picture of the legend below, so it is hidden from
            screen readers, which read the legend's numbers instead. */}
        <div aria-hidden="true" className="relative flex h-8 overflow-hidden rounded-md bg-surface-sunken">
          {parts.map((p, i) =>
            p.hundredths > 0 ? (
              <div
                key={i}
                title={`${p.number}. ${p.label}: ${formatPoints(p.hundredths)}`}
                className={`flex shrink-0 items-center justify-center border-r-2 border-surface font-mono text-xs font-medium transition-[width] duration-200 last:border-r-0 ${tones[i % tones.length]}`}
                style={{ width: pct(p.hundredths) }}
              >
                {p.hundredths / scale > 0.06 ? p.number : null}
              </div>
            ) : null,
          )}
          {state === "under" && (
            <div
              className="flex grow items-center justify-center text-xs text-muted-foreground"
              style={{
                backgroundImage:
                  "repeating-linear-gradient(135deg, transparent 0 5px, color-mix(in oklch, var(--border-strong), transparent 40%) 5px 6px)",
              }}
            >
              {difference / scale > 0.12 ? `${formatPoints(difference)} free` : null}
            </div>
          )}
          {state === "over" && (
            // The 100 mark, drawn over the segments.
            <div className="absolute inset-y-0 w-0.5 bg-destructive" style={{ left: pct(target) }} />
          )}
        </div>
        <div aria-hidden="true" className="relative mt-1 h-4 font-mono text-xs text-muted-foreground">
          <span className="absolute left-0">0</span>
          <span className="absolute -translate-x-full" style={{ left: pct(target) }}>
            100
          </span>
        </div>
      </div>

      <div role="status" aria-live="polite" className={`flex gap-2 ${status.tone}`}>
        <Icon className="mt-0.5 size-4 shrink-0" aria-hidden="true" />
        <div>
          <p className="text-sm font-medium">{status.title}</p>
          <p className="text-xs text-text-secondary">{status.body}</p>
        </div>
      </div>

      {parts.length > 0 && (
        <ol aria-label="Weight by criterion" className="flex flex-col gap-1 border-t pt-3 text-sm">
          {parts.map((p, i) => (
            <li key={i} className="flex items-center gap-2">
              <span
                aria-hidden="true"
                className={`flex size-5 shrink-0 items-center justify-center rounded font-mono text-xs ${tones[i % tones.length]}`}
              >
                {p.number}
              </span>
              <span className="min-w-0 flex-1 truncate">{p.label}</span>
              <span className="font-mono tabular-nums">{formatPoints(p.hundredths)}</span>
            </li>
          ))}
        </ol>
      )}

      <div className="border-t pt-3">{action}</div>
    </section>
  );
}
