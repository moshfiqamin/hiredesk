import {
  BadgeCheckIcon,
  CircleXIcon,
  InboxIcon,
  MessagesSquareIcon,
  StarIcon,
  type LucideIcon,
} from "lucide-react";
import { cn } from "cn";

import type { ApplicationStatus, Assignee } from "@/lib/api/types";

// Each status has its own icon as well as its word. The pipeline moves left
// to right, and only "offer" (a good outcome) and "rejected" (a final one)
// get a stronger treatment; the rest stay neutral so the score column keeps
// the eye.
const statuses: Record<ApplicationStatus, { label: string; icon: LucideIcon; className: string }> = {
  submitted: { label: "Submitted", icon: InboxIcon, className: "text-text-secondary" },
  shortlisted: { label: "Shortlisted", icon: StarIcon, className: "text-foreground" },
  interview: { label: "Interview", icon: MessagesSquareIcon, className: "text-foreground" },
  offer: { label: "Offer", icon: BadgeCheckIcon, className: "border-foreground/60 font-semibold text-foreground" },
  rejected: { label: "Rejected", icon: CircleXIcon, className: "border-transparent bg-surface-sunken text-muted-foreground" },
};

/** ApplicationStatusBadge shows where an application is in the pipeline, by icon and word. */
export function ApplicationStatusBadge({ status }: { status: ApplicationStatus }) {
  const { label, icon: Icon, className } = statuses[status];
  return (
    <span className={cn("inline-flex h-6 items-center gap-1.5 rounded-full border px-2 text-xs whitespace-nowrap", className)}>
      <Icon className="size-3.5" aria-hidden="true" />
      {label}
    </span>
  );
}

/** band names the score band, with the same thresholds as the design tokens' specimen. */
export function band(percentage: number): "strong" | "adequate" | "weak" {
  return percentage >= 75 ? "strong" : percentage >= 50 ? "adequate" : "weak";
}

const bandFill = {
  // Solid, hatched and outlined: the three bands differ in fill pattern as
  // well as hue, as the score-band chip in globals.css does.
  strong: "bg-band-strong",
  adequate:
    "bg-[repeating-linear-gradient(135deg,var(--band-adequate)_0_2px,color-mix(in_oklch,var(--band-adequate),transparent_55%)_2px_4px)]",
  weak: "border border-dashed border-band-weak bg-band-weak-bg",
} as const;

/**
 * ScoreCell is the score as a number and a bar. The number is the fact; the
 * bar lets a column of them be compared at a glance, and its fill pattern
 * repeats the band.
 */
export function ScoreCell({ percentage }: { percentage: number | null | undefined }) {
  if (percentage === null || percentage === undefined) {
    return <span className="text-sm text-muted-foreground">Not scored</span>;
  }
  const b = band(percentage);
  return (
    <span className="inline-flex items-center gap-2.5">
      <span className="w-12 text-right font-mono text-sm font-medium tabular-nums">{percentage.toFixed(1)}%</span>
      <span aria-hidden="true" className="relative h-2 w-20 overflow-hidden rounded-full bg-surface-sunken">
        <span className={cn("absolute inset-y-0 left-0 rounded-full", bandFill[b])} style={{ width: `${Math.max(percentage, 2)}%` }} />
      </span>
      <span className="sr-only">, {b}</span>
    </span>
  );
}

function initials(name: string): string {
  return name
    .split(/\s+/)
    .filter(Boolean)
    .slice(0, 2)
    .map((w) => w[0]?.toUpperCase())
    .join("");
}

/** AssigneeAvatars shows up to three interviewers as initials, then "+N". */
export function AssigneeAvatars({ assignees }: { assignees: Assignee[] | undefined }) {
  if (!assignees || assignees.length === 0) {
    return <span className="text-sm text-muted-foreground">Unassigned</span>;
  }
  const shown = assignees.slice(0, 3);
  const extra = assignees.length - shown.length;
  const names = assignees.map((a) => a.name).join(", ");
  return (
    // One accessible name for the group; the initials alone would be read as
    // letters.
    <span role="img" aria-label={`Assigned: ${names}`} title={names} className="flex -space-x-1.5">
      {shown.map((a) => (
        <span
          key={a.id}
          aria-hidden="true"
          className="flex size-6 items-center justify-center rounded-full border-2 border-surface bg-primary-soft text-[0.625rem] font-semibold text-primary-ink"
        >
          {initials(a.name)}
        </span>
      ))}
      {extra > 0 && (
        <span
          aria-hidden="true"
          className="flex size-6 items-center justify-center rounded-full border-2 border-surface bg-surface-sunken text-[0.625rem] font-medium text-text-secondary"
        >
          +{extra}
        </span>
      )}
    </span>
  );
}
