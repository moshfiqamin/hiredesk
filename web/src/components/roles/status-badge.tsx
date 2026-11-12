import { CircleDashedIcon, CircleDotIcon, LockIcon, type LucideIcon } from "lucide-react";
import { cn } from "cn";

import type { JobPostStatus } from "@/lib/api/types";

// Each status differs in three ways besides colour: the icon's shape, the
// border, and the fill. Read in greyscale, or by someone who cannot tell the
// colours apart, they are still three different-looking badges with a word.
const statuses: Record<JobPostStatus, { label: string; icon: LucideIcon; className: string }> = {
  // Unfinished: a dashed outline and a hollow, broken circle.
  draft: {
    label: "Draft",
    icon: CircleDashedIcon,
    className: "border-dashed border-border-strong text-muted-foreground",
  },
  // Live: the strongest of the three, a solid edge and a filled dot, because
  // an open role is the one an admin acts on.
  open: {
    label: "Open",
    icon: CircleDotIcon,
    className: "border-foreground/70 bg-surface-selected font-semibold text-foreground",
  },
  // Finished: no edge, a sunken fill and a lock, since closing is final.
  closed: {
    label: "Closed",
    icon: LockIcon,
    className: "border-transparent bg-surface-sunken text-text-secondary",
  },
};

/** StatusBadge shows a job post's status as an icon, a word and a border style. */
export function StatusBadge({ status, className }: { status: JobPostStatus; className?: string }) {
  const { label, icon: Icon, className: tone } = statuses[status];
  return (
    <span
      className={cn(
        "inline-flex h-6 shrink-0 items-center gap-1.5 rounded-full border px-2 text-xs whitespace-nowrap",
        tone,
        className,
      )}
    >
      <Icon className="size-3.5" aria-hidden="true" />
      {label}
    </span>
  );
}
