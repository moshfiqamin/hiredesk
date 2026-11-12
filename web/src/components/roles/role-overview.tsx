"use client";

import { useParams } from "next/navigation";

import { Skeleton } from "@/components/ui/skeleton";
import { useJobPost } from "@/hooks/use-job-posts";
import type { JobPost } from "@/lib/api/types";
import { employmentTypeLabel, formatDate, formatNumber } from "@/lib/format";

function salaryRange(post: JobPost): string | null {
  const { salary_min: min, salary_max: max } = post;
  if (min !== null && max !== null) return `${formatNumber(min)} – ${formatNumber(max)}`;
  if (min !== null) return `From ${formatNumber(min)}`;
  if (max !== null) return `Up to ${formatNumber(max)}`;
  return null;
}

/** RoleOverview is the read-only summary of one role. */
export function RoleOverview() {
  const { id } = useParams<{ id: string }>();
  const { data: post } = useJobPost(id);

  // RoleShell shows the error state; here only loading is left to handle.
  if (!post) {
    return (
      <div className="grid gap-6 lg:grid-cols-[1fr_18rem]" aria-busy="true" aria-label="Loading the role">
        <Skeleton className="h-40" />
        <Skeleton className="h-56" />
      </div>
    );
  }

  const facts: [string, string | null][] = [
    ["Employment type", post.employment_type ? employmentTypeLabel(post.employment_type) : null],
    ["Location", post.location || null],
    ["Vacancies", formatNumber(post.vacancies)],
    ["Salary", salaryRange(post)],
    ["Closes on", post.closes_at ? formatDate(post.closes_at) : null],
    ["Created", formatDate(post.created_at)],
  ];

  return (
    <div className="grid items-start gap-6 lg:grid-cols-[1fr_18rem]">
      <section aria-labelledby="description-heading" className="rounded-lg border bg-surface p-5">
        <h2 id="description-heading" className="mb-2 text-sm font-medium text-muted-foreground">
          Description
        </h2>
        {post.description ? (
          // whitespace-pre-line keeps the paragraphs the admin typed.
          <p className="max-w-prose text-md whitespace-pre-line">{post.description}</p>
        ) : (
          <p className="text-muted-foreground">No description yet. Candidates see this before they apply.</p>
        )}
      </section>
      <dl className="grid gap-3 rounded-lg border bg-surface p-5 text-sm">
        {facts.map(([label, value]) => (
          <div key={label} className="flex justify-between gap-4">
            <dt className="text-muted-foreground">{label}</dt>
            <dd className={`text-right ${value ? "tabular" : "text-muted-foreground"}`}>{value ?? "—"}</dd>
          </div>
        ))}
      </dl>
    </div>
  );
}
