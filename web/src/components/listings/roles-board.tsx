"use client";

import { ArrowRightIcon, BriefcaseIcon, MapPinIcon, SearchXIcon } from "lucide-react";
import Link from "next/link";

import { EmptyState, ErrorState } from "@/components/states/states";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { useListings } from "@/hooks/use-listings";
import type { JobListing } from "@/lib/api/types";
import { employmentTypeLabel, placesLeft } from "@/lib/format";

/**
 * RolesBoard is the public listing: every open role as a card. It is the one
 * page a stranger sees, so it leads with what a job seeker scans for
 * (title, where, what kind of contract, what skills) and says plainly how
 * many places are left.
 */
export function RolesBoard() {
  const listings = useListings();
  const roles = listings.data?.pages.flatMap((p) => p.data) ?? [];

  return (
    <div className="flex flex-col gap-8">
      <header className="flex max-w-2xl flex-col gap-3">
        <p className="text-sm font-medium text-primary-ink">Hiring now</p>
        <h1 className="text-3xl font-semibold tracking-tight text-balance sm:text-4xl">Open roles</h1>
        <p className="text-md text-text-secondary">
          Every application is read in full and scored against the same published criteria, so you know what each
          role is looking for before you apply.
        </p>
      </header>

      {listings.isError && !listings.data ? (
        <ErrorState error={listings.error} title="Could not load the open roles" onRetry={() => void listings.refetch()} />
      ) : listings.isPending ? (
        <ul className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3" aria-busy="true" aria-label="Loading open roles">
          {Array.from({ length: 6 }, (_, i) => (
            <li key={i}>
              <Skeleton className="h-48 rounded-xl" />
            </li>
          ))}
        </ul>
      ) : roles.length === 0 ? (
        <EmptyState icon={SearchXIcon} title="No open roles right now">
          New roles appear here as soon as they open. Check back soon.
        </EmptyState>
      ) : (
        <>
          {/* Announced once, when the count changes: "Load more" adds cards
              a screen reader would otherwise not hear about. */}
          <p className="text-sm text-text-secondary" role="status">
            {roles.length} open {roles.length === 1 ? "role" : "roles"}
            {listings.hasNextPage ? " shown" : ""}
          </p>
          <ul className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
            {roles.map((role) => (
              <li key={role.id} className="flex">
                <RoleCard role={role} />
              </li>
            ))}
          </ul>
          {listings.hasNextPage && (
            <div className="flex justify-center">
              <Button variant="outline" onClick={() => void listings.fetchNextPage()} disabled={listings.isFetchingNextPage}>
                {listings.isFetchingNextPage ? "Loading…" : "Show more roles"}
              </Button>
            </div>
          )}
        </>
      )}
    </div>
  );
}

const TAGS_SHOWN = 5;

function RoleCard({ role }: { role: JobListing }) {
  const extra = role.skill_tags.length - TAGS_SHOWN;
  return (
    // One link for the whole card, stretched over it with an ::after, so the
    // card is a single tab stop and a single announcement rather than a
    // cluster of links that all go to the same place.
    <article className="group relative flex w-full flex-col gap-4 rounded-xl border bg-surface p-5 transition-colors hover:border-border-strong has-[a:focus-visible]:ring-2 has-[a:focus-visible]:ring-ring">
      <div className="flex flex-col gap-2">
        <h2 className="text-lg leading-snug font-semibold tracking-tight">
          <Link href={`/roles/${role.id}`} className="outline-none after:absolute after:inset-0 after:rounded-xl">
            {role.title}
          </Link>
        </h2>
        <p className="flex flex-wrap items-center gap-x-4 gap-y-1 text-sm text-text-secondary">
          {role.location && (
            <span className="inline-flex items-center gap-1.5">
              <MapPinIcon className="size-3.5" aria-hidden="true" />
              {role.location}
            </span>
          )}
          {role.employment_type && (
            <span className="inline-flex items-center gap-1.5">
              <BriefcaseIcon className="size-3.5" aria-hidden="true" />
              {employmentTypeLabel(role.employment_type)}
            </span>
          )}
        </p>
      </div>

      {role.skill_tags.length > 0 && (
        <ul aria-label="Skills" className="flex flex-wrap gap-1.5">
          {role.skill_tags.slice(0, TAGS_SHOWN).map((tag) => (
            <li key={tag} className="rounded-full bg-surface-sunken px-2.5 py-0.5 font-mono text-xs text-text-secondary">
              {tag}
            </li>
          ))}
          {extra > 0 && <li className="px-1 py-0.5 text-xs text-muted-foreground">+{extra} more</li>}
        </ul>
      )}

      <div className="mt-auto flex items-center justify-between gap-3 border-t pt-4 text-sm">
        <span className={role.remaining_vacancies > 0 ? "font-medium text-foreground" : "text-muted-foreground"}>
          {placesLeft(role.remaining_vacancies)}
        </span>
        <span aria-hidden="true" className="inline-flex items-center gap-1 text-primary-ink">
          View role
          <ArrowRightIcon className="size-3.5 transition-transform group-hover:translate-x-0.5 motion-reduce:transition-none" />
        </span>
      </div>
    </article>
  );
}
