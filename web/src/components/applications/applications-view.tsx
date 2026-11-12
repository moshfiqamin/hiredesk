"use client";

import { ArrowDownWideNarrowIcon, InboxIcon, SearchIcon, SearchXIcon, XIcon } from "lucide-react";
import Link from "next/link";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { useMemo, useState } from "react";
import { toast } from "sonner";

import { EmptyState, ErrorState } from "@/components/states/states";
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
import { Button, buttonVariants } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { NativeSelect } from "@/components/ui/native-select";
import { Skeleton } from "@/components/ui/skeleton";
import { useApplicationSearch, useApplications, useBulkStatus } from "@/hooks/use-applications";
import { useDebouncedCallback } from "@/hooks/use-debounced-callback";
import { useJobPosts } from "@/hooks/use-job-posts";
import type { ApplicationStatus, Assignee } from "@/lib/api/types";
import {
  PAGE_SIZE,
  applicationStatuses,
  isSearching,
  parseView,
  viewToSearch,
  withFilters,
  type ApplicationsView as View,
} from "@/lib/applications/view";
import { formatDate, formatNumber } from "@/lib/format";

import { ApplicationStatusBadge, AssigneeAvatars, ScoreCell } from "./cells";

/**
 * SEARCH_DEBOUNCE_MS is how long typing must pause before a search is sent.
 * 300ms sits just above the gap between keystrokes of a steady typist
 * (roughly 150–250ms), so a word is sent once, when the typist pauses, yet
 * the results still feel like they follow the typing.
 */
const SEARCH_DEBOUNCE_MS = 300;

/** Row is what the table shows, from either the list or the search endpoint. */
interface Row {
  id: string;
  name: string;
  role: string;
  percentage: number | null | undefined;
  status: ApplicationStatus;
  submittedAt: string;
  assignees: Assignee[] | undefined;
}

/** ApplicationsView is the admin applications table. Its whole state is in the URL. */
export function ApplicationsView() {
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();
  // Derived from the URL on every render: the URL is the single source of
  // truth, so there is no second copy of the filters to fall out of step.
  const view = useMemo(() => parseView(new URLSearchParams(searchParams.toString())), [searchParams]);

  const roles = useJobPosts();
  const list = useApplications(view);
  const search = useApplicationSearch(view);
  const searching = isSearching(view);

  /**
   * go writes a view to the URL. push adds a history entry, so Back returns
   * to the previous filters or page; replace overwrites the current one,
   * used for search typing so each pause does not become a Back step.
   */
  function go(next: View, mode: "push" | "replace" = "push") {
    router[mode](`${pathname}${viewToSearch(next)}`, { scroll: false });
  }

  // The search box is local state, updated on every keystroke so typing is
  // instant. Writing the term to the URL, which is what sends a request, is
  // debounced.
  const [input, setInput] = useState(view.q);
  // The q this component last agreed with the URL on. State, not a ref,
  // because the check below reads it during render.
  const [syncedQ, setSyncedQ] = useState(view.q);

  const writeQuery = useDebouncedCallback((q: string) => {
    // Read from the address bar at the moment of writing: other filters may
    // have changed since the keystroke that scheduled this.
    const current = parseView(new URLSearchParams(window.location.search));
    if (q.trim() === current.q.trim()) return;
    // Recorded before navigating, so the URL change that follows is known to
    // be ours and does not reset the box while the user keeps typing.
    setSyncedQ(q.trim());
    go(withFilters(current, { q }), "replace");
  }, SEARCH_DEBOUNCE_MS);

  function onSearchInput(q: string) {
    setInput(q);
    writeQuery(q);
  }

  // Back and Forward change the URL without touching the box. Adjusting
  // state during render, when what it mirrors has changed, is React's
  // documented alternative to an effect for this.
  if (view.q !== syncedQ) {
    setSyncedQ(view.q);
    setInput(view.q);
  }

  const roleTitles = useMemo(() => new Map((roles.data ?? []).map((r) => [r.id, r.title])), [roles.data]);
  const roleTitle = view.role ? roleTitles.get(view.role) : undefined;

  const active = searching ? search : list;
  const rows: Row[] | undefined = searching
    ? search.data?.data.map((e) => ({
        id: e.application_id,
        name: e.candidate_name,
        role: roleTitle ?? "",
        percentage: e.percentage,
        status: e.status,
        submittedAt: e.submitted_at,
        assignees: e.assignees,
      }))
    : list.data?.data.map((a) => ({
        id: a.id,
        name: a.candidate_name,
        role: a.job_post_title,
        percentage: a.score?.percentage,
        status: a.status,
        submittedAt: a.submitted_at,
        assignees: a.assignees,
      }));
  const total = searching ? search.data?.total : list.data?.total;
  const filtered = view.status !== undefined || view.role !== undefined || searching;

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-col gap-3 lg:flex-row lg:items-end">
        <div className="grid flex-1 gap-3 sm:grid-cols-3">
          <div className="flex flex-col gap-1.5">
            <label htmlFor="filter-role" className="text-sm font-medium">
              Role
            </label>
            <NativeSelect
              id="filter-role"
              value={view.role ?? ""}
              onChange={(e) => go(withFilters(view, { role: e.target.value || undefined }))}
            >
              <option value="">All roles</option>
              {(roles.data ?? []).map((r) => (
                <option key={r.id} value={r.id}>
                  {r.title}
                </option>
              ))}
            </NativeSelect>
          </div>
          <div className="flex flex-col gap-1.5">
            <label htmlFor="filter-status" className="text-sm font-medium">
              Status
            </label>
            <NativeSelect
              id="filter-status"
              value={view.status ?? ""}
              disabled={searching}
              onChange={(e) => go(withFilters(view, { status: (e.target.value || undefined) as ApplicationStatus | undefined }))}
            >
              <option value="">Any status</option>
              {applicationStatuses.map((s) => (
                <option key={s.value} value={s.value}>
                  {s.label}
                </option>
              ))}
            </NativeSelect>
          </div>
          <div className="flex flex-col gap-1.5">
            <label htmlFor="filter-sort" className="text-sm font-medium">
              Sort
            </label>
            <NativeSelect
              id="filter-sort"
              value={view.sort}
              disabled={searching}
              onChange={(e) => go(withFilters(view, { sort: e.target.value === "score" ? "score" : "newest" }))}
            >
              <option value="newest">Newest first</option>
              <option value="score">Highest score first</option>
            </NativeSelect>
          </div>
        </div>
        <div className="flex flex-col gap-1.5 lg:w-80">
          <label htmlFor="filter-q" className="text-sm font-medium">
            Search answers
          </label>
          <div className="relative">
            <SearchIcon className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" aria-hidden="true" />
            <Input
              id="filter-q"
              type="search"
              maxLength={200}
              placeholder={view.role ? "Words from the answers" : "Choose a role to search"}
              value={input}
              aria-describedby="filter-q-hint"
              onChange={(e) => onSearchInput(e.target.value)}
              className="pl-8"
            />
          </div>
        </div>
      </div>
      <p id="filter-q-hint" className="-mt-1 text-xs text-muted-foreground">
        {input.trim() && !view.role
          ? "Search looks inside one role's answers. Choose a role to run it."
          : searching
            ? "Most relevant first. Status and sort apply to the full list, not to search results."
            : "Search finds every word, in any form: “testing” also finds “tested”."}
      </p>

      {active.isError && !active.data ? (
        <ErrorState error={active.error} title="Could not load applications" onRetry={() => void active.refetch()} retrying={active.isFetching} />
      ) : !rows ? (
        <TableSkeleton />
      ) : rows.length === 0 ? (
        <EmptyResults view={view} roleTitle={roleTitle} filtered={filtered} onClear={() => go(withFilters(view, { status: undefined, role: undefined, q: "" }))} onClearSearch={() => onSearchInput("")} />
      ) : (
        <>
          {/* Remounted for each view (the key), so a selection never carries
              over to rows the admin has not seen. */}
          <ResultsTable
            key={viewToSearch(view)}
            rows={rows}
            view={view}
            searching={searching}
            stale={active.isPlaceholderData}
          />
          <Pager
            view={view}
            total={total ?? 0}
            shown={rows.length}
            nextCursor={searching ? null : (list.data?.next_cursor ?? null)}
            nextOffset={searching ? (search.data?.next_offset ?? null) : null}
            onGo={(next) => go(next)}
          />
        </>
      )}
    </div>
  );
}

function ResultsTable({ rows, view, searching, stale }: { rows: Row[]; view: View; searching: boolean; stale: boolean }) {
  const [selected, setSelected] = useState<Set<string>>(() => new Set());
  const ranked = !searching && view.sort === "score";
  const firstRank = (view.page - 1) * PAGE_SIZE + 1;
  const allSelected = selected.size === rows.length;

  function toggle(id: string, on: boolean) {
    setSelected((prev) => {
      const next = new Set(prev);
      if (on) next.add(id);
      else next.delete(id);
      return next;
    });
  }

  return (
    <div className="flex flex-col gap-3">
      {selected.size > 0 && (
        <BulkBar rows={rows.filter((r) => selected.has(r.id))} onDone={() => setSelected(new Set())} />
      )}
      <div
        aria-busy={stale}
        className={`overflow-x-auto rounded-lg border bg-surface transition-opacity ${stale ? "opacity-60" : ""}`}
      >
        <table className="w-full min-w-[56rem] text-sm">
          <caption className="sr-only">
            Applications{searching ? `, search results for ${view.q}` : ""}, {ranked ? "highest score first" : "newest first"}
          </caption>
          <thead className="border-b bg-surface-sunken/60 text-xs tracking-wide text-muted-foreground uppercase">
            <tr>
              <th scope="col" className="w-10 px-3 py-2">
                <Checkbox
                  aria-label={allSelected ? "Deselect all on this page" : "Select all on this page"}
                  checked={allSelected}
                  // Some but not all: the third state of a select-all box.
                  indeterminate={selected.size > 0 && !allSelected}
                  onCheckedChange={(on) => setSelected(on ? new Set(rows.map((r) => r.id)) : new Set())}
                />
              </th>
              {ranked && (
                <th scope="col" className="w-12 px-2 py-2 text-right font-medium">
                  Rank
                </th>
              )}
              <th scope="col" className="px-3 py-2 text-left font-medium">
                Candidate
              </th>
              <th scope="col" className="px-3 py-2 text-left font-medium">
                Score
                {ranked && <ArrowDownWideNarrowIcon className="ml-1 inline size-3.5 align-[-2px]" aria-hidden="true" />}
              </th>
              <th scope="col" className="px-3 py-2 text-left font-medium">
                Status
              </th>
              <th scope="col" className="px-3 py-2 text-left font-medium">
                Role
              </th>
              <th scope="col" className="px-3 py-2 text-right font-medium">
                Submitted
              </th>
              <th scope="col" className="px-3 py-2 text-left font-medium">
                Interviewers
              </th>
            </tr>
          </thead>
          <tbody className="divide-y">
            {rows.map((row, i) => (
              <tr key={row.id} className="hover:bg-surface-hover has-data-checked:bg-surface-selected">
                <td className="px-3 py-2.5">
                  <Checkbox
                    aria-label={`Select ${row.name}`}
                    checked={selected.has(row.id)}
                    onCheckedChange={(on) => toggle(row.id, on)}
                  />
                </td>
                {ranked && (
                  <td className="px-2 py-2.5 text-right font-mono text-xs text-muted-foreground tabular-nums">
                    {formatNumber(firstRank + i)}
                  </td>
                )}
                <td className="px-3 py-2.5">
                  <Link href={`/admin/applications/${row.id}`} className="font-medium hover:underline">
                    {row.name}
                  </Link>
                </td>
                <td className="px-3 py-2.5">
                  <ScoreCell percentage={row.percentage} />
                </td>
                <td className="px-3 py-2.5">
                  <ApplicationStatusBadge status={row.status} />
                </td>
                <td className="max-w-56 truncate px-3 py-2.5 text-text-secondary">{row.role}</td>
                <td className="px-3 py-2.5 text-right whitespace-nowrap text-text-secondary">
                  <time dateTime={row.submittedAt}>{formatDate(row.submittedAt)}</time>
                </td>
                <td className="px-3 py-2.5">
                  <AssigneeAvatars assignees={row.assignees} />
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}

/** BulkBar changes the status of the selected applications, after a confirmation that names how many. */
function BulkBar({ rows, onDone }: { rows: Row[]; onDone: () => void }) {
  const bulk = useBulkStatus();
  const [target, setTarget] = useState<ApplicationStatus | "">("");
  const [confirming, setConfirming] = useState(false);
  const label = applicationStatuses.find((s) => s.value === target)?.label;
  const count = rows.length;
  const noun = `${formatNumber(count)} ${count === 1 ? "application" : "applications"}`;

  async function apply() {
    if (!target) return;
    setConfirming(false);
    const result = await bulk.mutateAsync({ rows: rows.map((r) => ({ id: r.id, name: r.name })), status: target });
    onDone();
    if (result.refused.length === 0) {
      toast.success(`Moved ${formatNumber(result.changed)} to ${label}`);
    } else {
      toast.warning(`Moved ${formatNumber(result.changed)} of ${noun} to ${label}`, {
        description: result.refused
          .slice(0, 3)
          .map((r) => `${r.name}: ${r.reason}`)
          .concat(result.refused.length > 3 ? [`and ${result.refused.length - 3} more`] : [])
          .join(" · "),
        duration: 10_000,
      });
    }
  }

  return (
    <div role="region" aria-label="Bulk actions" className="flex flex-wrap items-center gap-3 rounded-lg border border-primary/40 bg-surface-selected px-3 py-2">
      <span className="text-sm font-medium" aria-live="polite">
        {noun} selected
      </span>
      <div className="flex items-center gap-2">
        <label htmlFor="bulk-status" className="sr-only">
          New status
        </label>
        <NativeSelect
          id="bulk-status"
          value={target}
          onChange={(e) => setTarget(e.target.value as ApplicationStatus | "")}
          className="w-44 bg-surface"
        >
          <option value="">Move to…</option>
          {applicationStatuses.map((s) => (
            <option key={s.value} value={s.value}>
              {s.label}
            </option>
          ))}
        </NativeSelect>
        <Button size="sm" disabled={!target || bulk.isPending} onClick={() => setConfirming(true)}>
          {bulk.isPending ? "Updating…" : "Apply"}
        </Button>
      </div>
      <Button variant="ghost" size="sm" className="ml-auto" onClick={onDone}>
        <XIcon aria-hidden="true" />
        Clear selection
      </Button>

      <AlertDialog open={confirming} onOpenChange={setConfirming}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              Move {noun} to {label}?
            </AlertDialogTitle>
            <AlertDialogDescription>
              Each one moves only if its current status allows it; the rest are left as they are and listed afterwards.
              {target === "rejected" && " Rejection is final."}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction variant={target === "rejected" ? "destructive" : "default"} onClick={() => void apply()}>
              Move {noun}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

/**
 * Pager pages through the results. The list is keyset-paged: it can go to
 * the next page or back to the first, and Back in the browser returns to the
 * previous page, because each page is a URL. Search is offset-paged and can
 * go either way.
 */
function Pager({
  view,
  total,
  shown,
  nextCursor,
  nextOffset,
  onGo,
}: {
  view: View;
  total: number;
  shown: number;
  nextCursor: string | null;
  nextOffset: number | null;
  onGo: (next: View) => void;
}) {
  const searching = isSearching(view);
  const start = searching ? view.offset + 1 : (view.page - 1) * PAGE_SIZE + 1;
  const end = start + shown - 1;

  return (
    <nav aria-label="Pages" className="flex flex-wrap items-center justify-between gap-3 text-sm">
      <p className="text-text-secondary tabular">
        Showing {formatNumber(start)}–{formatNumber(end)} of {formatNumber(total)}
      </p>
      <div className="flex gap-2">
        {searching ? (
          <Button variant="outline" size="sm" disabled={view.offset === 0} onClick={() => onGo({ ...view, offset: Math.max(0, view.offset - PAGE_SIZE) })}>
            Previous
          </Button>
        ) : (
          <Button variant="outline" size="sm" disabled={view.page === 1} onClick={() => onGo({ ...view, after: undefined, page: 1 })}>
            First page
          </Button>
        )}
        <Button
          variant="outline"
          size="sm"
          disabled={searching ? nextOffset === null : nextCursor === null}
          onClick={() =>
            onGo(searching ? { ...view, offset: nextOffset ?? view.offset } : { ...view, after: nextCursor ?? undefined, page: view.page + 1 })
          }
        >
          Next page
        </Button>
      </div>
    </nav>
  );
}

/** EmptyResults explains an empty table in terms of what was asked for. */
function EmptyResults({
  view,
  roleTitle,
  filtered,
  onClear,
  onClearSearch,
}: {
  view: View;
  roleTitle: string | undefined;
  filtered: boolean;
  onClear: () => void;
  onClearSearch: () => void;
}) {
  if (isSearching(view)) {
    return (
      <EmptyState
        icon={SearchXIcon}
        title={`No answers match “${view.q.trim()}”`}
        action={
          <Button variant="outline" onClick={onClearSearch}>
            Clear search
          </Button>
        }
      >
        Searched every answer to {roleTitle ?? "this role"}. Every word has to appear, so fewer words find more.
      </EmptyState>
    );
  }

  if (!filtered) {
    return (
      <EmptyState
        icon={InboxIcon}
        title="No applications yet"
        action={
          <Link href="/admin/roles" className={buttonVariants({ variant: "outline" })}>
            Go to roles
          </Link>
        }
      >
        Applications appear here as candidates apply to an open role, each scored against that role&apos;s rubric the
        moment it arrives.
      </EmptyState>
    );
  }

  const status = applicationStatuses.find((s) => s.value === view.status)?.label.toLowerCase();
  const title = [
    "No",
    status ?? "",
    "applications",
    roleTitle ? `for ${roleTitle}` : "",
  ]
    .filter(Boolean)
    .join(" ");

  return (
    <EmptyState
      icon={SearchXIcon}
      title={title}
      action={
        <Button variant="outline" onClick={onClear}>
          Clear filters
        </Button>
      }
    >
      {view.status === "offer"
        ? "No one has been made an offer yet."
        : view.status
          ? `Nothing is at the ${status} stage right now.`
          : "This role has not received any applications yet."}
    </EmptyState>
  );
}

/** TableSkeleton has the table's columns, so nothing shifts when rows arrive. */
function TableSkeleton() {
  return (
    <div aria-busy="true" aria-label="Loading applications" className="overflow-hidden rounded-lg border bg-surface">
      <div className="h-9 border-b bg-surface-sunken/60" />
      {Array.from({ length: 8 }, (_, i) => (
        <div key={i} className="flex h-12 items-center gap-6 border-b px-3 last:border-0">
          <Skeleton className="size-4" />
          <Skeleton className={`h-3.5 ${i % 2 ? "w-36" : "w-44"}`} />
          <div className="flex items-center gap-2">
            <Skeleton className="h-3.5 w-12" />
            <Skeleton className="h-2 w-20 rounded-full" />
          </div>
          <Skeleton className="h-6 w-24 rounded-full" />
          <Skeleton className="h-3.5 w-40" />
          <Skeleton className="ml-auto h-3.5 w-20" />
          <Skeleton className="size-6 rounded-full" />
        </div>
      ))}
    </div>
  );
}
