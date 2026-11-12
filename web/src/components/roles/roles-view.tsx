"use client";

import {
  ArrowDownIcon,
  ArrowUpDownIcon,
  ArrowUpIcon,
  BriefcaseIcon,
  EyeIcon,
  LockIcon,
  MoreHorizontalIcon,
  PencilIcon,
  PlusIcon,
  RocketIcon,
  SearchIcon,
  SearchXIcon,
} from "lucide-react";
import Link from "next/link";
import { useMemo, useState } from "react";

import { EmptyState, QueryView } from "@/components/states/states";
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
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { useJobPosts, useTransitionJobPost, type TransitionTarget } from "@/hooks/use-job-posts";
import type { JobPostStatus, JobPostSummary } from "@/lib/api/types";
import { employmentTypeLabel, formatDate, formatNumber } from "@/lib/format";

import { StatusBadge } from "./status-badge";

type SortKey = "title" | "status" | "location" | "vacancies" | "applications" | "created";
type Sort = { key: SortKey; dir: "asc" | "desc" };
type StatusFilter = JobPostStatus | "all";

// Draft first: a draft is the role still needing work, so "status ascending"
// puts the to-do list at the top, then live roles, then finished ones.
const statusOrder: Record<JobPostStatus, number> = { draft: 0, open: 1, closed: 2 };

// One comparator per column. Each compares ascending; descending negates it.
const comparators: Record<SortKey, (a: JobPostSummary, b: JobPostSummary) => number> = {
  title: (a, b) => a.title.localeCompare(b.title),
  status: (a, b) => statusOrder[a.status] - statusOrder[b.status],
  location: (a, b) => (a.location ?? "").localeCompare(b.location ?? ""),
  vacancies: (a, b) => a.remaining_vacancies - b.remaining_vacancies || a.vacancies - b.vacancies,
  applications: (a, b) => a.application_count - b.application_count,
  created: (a, b) => a.created_at.localeCompare(b.created_at),
};

// A new column starts in the direction its data is usually read: text A→Z,
// numbers and dates biggest or newest first.
const firstDirection: Record<SortKey, Sort["dir"]> = {
  title: "asc",
  status: "asc",
  location: "asc",
  vacancies: "desc",
  applications: "desc",
  created: "desc",
};

/** visibleRoles filters by status and title, then sorts. Pure, so it can be memoised. */
function visibleRoles(posts: JobPostSummary[], status: StatusFilter, search: string, sort: Sort) {
  const needle = search.trim().toLocaleLowerCase();
  const sign = sort.dir === "asc" ? 1 : -1;
  return posts
    .filter((p) => status === "all" || p.status === status)
    .filter((p) => needle === "" || p.title.toLocaleLowerCase().includes(needle))
    // filter returns a new array, so sort (which works in place) never
    // reorders the cached list TanStack Query holds. The created_at tie-break
    // keeps equal rows in a stable, meaningful order.
    .sort((a, b) => sign * comparators[sort.key](a, b) || b.created_at.localeCompare(a.created_at));
}

/** RolesView is the admin roles list: filters, the table, and its states. */
export function RolesView() {
  const jobPosts = useJobPosts();
  const [status, setStatus] = useState<StatusFilter>("all");
  const [search, setSearch] = useState("");
  const [sort, setSort] = useState<Sort>({ key: "created", dir: "desc" });

  return (
    <QueryView
      query={jobPosts}
      errorTitle="Could not load roles"
      loading={<RolesSkeleton />}
      isEmpty={(posts) => posts.length === 0}
      empty={<FirstRoleEmptyState />}
    >
      {(posts) => (
        <RolesList
          posts={posts}
          status={status}
          onStatus={setStatus}
          search={search}
          onSearch={setSearch}
          sort={sort}
          onSort={setSort}
        />
      )}
    </QueryView>
  );
}

function RolesList({
  posts,
  status,
  onStatus,
  search,
  onSearch,
  sort,
  onSort,
}: {
  posts: JobPostSummary[];
  status: StatusFilter;
  onStatus: (s: StatusFilter) => void;
  search: string;
  onSearch: (s: string) => void;
  sort: Sort;
  onSort: (s: Sort) => void;
}) {
  // Recomputed only when one of its inputs changes, not on every render
  // (opening a menu re-renders this component, for instance).
  const rows = useMemo(() => visibleRoles(posts, status, search, sort), [posts, status, search, sort]);

  const counts = useMemo(() => {
    const c: Record<StatusFilter, number> = { all: posts.length, draft: 0, open: 0, closed: 0 };
    for (const p of posts) c[p.status]++;
    return c;
  }, [posts]);

  function toggleSort(key: SortKey) {
    onSort(sort.key === key ? { key, dir: sort.dir === "asc" ? "desc" : "asc" } : { key, dir: firstDirection[key] });
  }

  const filtered = status !== "all" || search.trim() !== "";

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-col gap-3 md:flex-row md:items-center md:justify-between">
        <StatusFilterBar value={status} counts={counts} onChange={onStatus} />
        <div className="relative md:w-72">
          <SearchIcon
            className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground"
            aria-hidden="true"
          />
          <label htmlFor="role-search" className="sr-only">
            Search roles by title
          </label>
          <Input
            id="role-search"
            type="search"
            placeholder="Search by title"
            value={search}
            onChange={(e) => onSearch(e.target.value)}
            className="pl-8"
          />
        </div>
      </div>

      {/* Announced by screen readers whenever the number of rows changes, so a
          filter that hides everything is not silent. */}
      <p className="sr-only" role="status" aria-live="polite">
        {rows.length} of {posts.length} roles shown
      </p>

      {rows.length === 0 ? (
        <EmptyState
          icon={SearchXIcon}
          title="No roles match"
          action={
            filtered && (
              <Button
                variant="outline"
                onClick={() => {
                  onStatus("all");
                  onSearch("");
                }}
              >
                Clear filters
              </Button>
            )
          }
        >
          {search.trim() ? <>Nothing titled like “{search.trim()}”</> : <>No roles are {status} right now</>}
          {status !== "all" && search.trim() ? <> among {status} roles</> : null}.
        </EmptyState>
      ) : (
        <>
          <RolesTable rows={rows} sort={sort} onSort={toggleSort} />
          <RolesCards rows={rows} />
        </>
      )}
    </div>
  );
}

function StatusFilterBar({
  value,
  counts,
  onChange,
}: {
  value: StatusFilter;
  counts: Record<StatusFilter, number>;
  onChange: (s: StatusFilter) => void;
}) {
  const options: { value: StatusFilter; label: string }[] = [
    { value: "all", label: "All" },
    { value: "draft", label: "Draft" },
    { value: "open", label: "Open" },
    { value: "closed", label: "Closed" },
  ];
  return (
    <div role="group" aria-label="Filter by status" className="inline-flex w-fit rounded-lg border bg-surface p-0.5">
      {options.map((o) => (
        <button
          key={o.value}
          type="button"
          // aria-pressed makes each a toggle button: a screen reader says
          // "Open, 3, pressed" for the active filter.
          aria-pressed={value === o.value}
          onClick={() => onChange(o.value)}
          className="inline-flex h-7 items-center gap-1.5 rounded-md px-2.5 text-sm text-text-secondary outline-none hover:text-foreground focus-visible:ring-3 focus-visible:ring-ring/50 aria-pressed:bg-surface-selected aria-pressed:font-medium aria-pressed:text-primary-ink"
        >
          {o.label}
          <span className="tabular text-xs text-muted-foreground">{counts[o.value]}</span>
        </button>
      ))}
    </div>
  );
}

function SortableHead({
  label,
  column,
  sort,
  onSort,
  align = "left",
}: {
  label: string;
  column: SortKey;
  sort: Sort;
  onSort: (key: SortKey) => void;
  align?: "left" | "right";
}) {
  const active = sort.key === column;
  const Icon = !active ? ArrowUpDownIcon : sort.dir === "asc" ? ArrowUpIcon : ArrowDownIcon;
  return (
    <th
      scope="col"
      // aria-sort belongs on the header cell, not the button: it describes
      // how the column is sorted, and only the sorted column carries it.
      aria-sort={active ? (sort.dir === "asc" ? "ascending" : "descending") : undefined}
      className={`h-9 px-3 font-medium text-muted-foreground ${align === "right" ? "text-right" : "text-left"}`}
    >
      <button
        type="button"
        onClick={() => onSort(column)}
        className={`-mx-1 inline-flex items-center gap-1 rounded px-1 text-xs tracking-wide uppercase outline-none hover:text-foreground focus-visible:ring-3 focus-visible:ring-ring/50 ${align === "right" ? "flex-row-reverse" : ""} ${active ? "text-foreground" : ""}`}
      >
        {label}
        <Icon className={`size-3.5 ${active ? "" : "opacity-40"}`} aria-hidden="true" />
      </button>
    </th>
  );
}

function RolesTable({ rows, sort, onSort }: { rows: JobPostSummary[]; sort: Sort; onSort: (key: SortKey) => void }) {
  return (
    // Hidden below md (768px), where the cards take over; a seven-column
    // table at 375px would scroll sideways, hiding columns off-screen.
    <div className="hidden overflow-hidden rounded-lg border bg-surface md:block">
      <table className="w-full text-sm">
        <caption className="sr-only">Roles, sorted by {sort.key}, {sort.dir === "asc" ? "ascending" : "descending"}</caption>
        <thead className="border-b bg-surface-sunken/60">
          <tr>
            <SortableHead label="Role" column="title" sort={sort} onSort={onSort} />
            <SortableHead label="Status" column="status" sort={sort} onSort={onSort} />
            <SortableHead label="Location" column="location" sort={sort} onSort={onSort} />
            <SortableHead label="Vacancies" column="vacancies" sort={sort} onSort={onSort} align="right" />
            <SortableHead label="Applications" column="applications" sort={sort} onSort={onSort} align="right" />
            <SortableHead label="Created" column="created" sort={sort} onSort={onSort} align="right" />
            <th scope="col" className="w-12 px-3">
              <span className="sr-only">Actions</span>
            </th>
          </tr>
        </thead>
        <tbody className="divide-y">
          {rows.map((post) => (
            <tr key={post.id} className="hover:bg-surface-hover">
              <td className="max-w-80 px-3 py-2.5">
                <Link href={`/admin/roles/${post.id}`} className="block truncate font-medium hover:underline">
                  {post.title}
                </Link>
                {post.employment_type && (
                  <span className="block truncate text-xs text-muted-foreground">
                    {employmentTypeLabel(post.employment_type)}
                  </span>
                )}
              </td>
              <td className="px-3 py-2.5">
                <StatusBadge status={post.status} />
              </td>
              <td className="px-3 py-2.5 text-text-secondary">{post.location ?? <Unset />}</td>
              <td className="px-3 py-2.5 text-right">
                <Vacancies post={post} />
              </td>
              <td className="px-3 py-2.5 text-right font-mono">{formatNumber(post.application_count)}</td>
              <td className="px-3 py-2.5 text-right whitespace-nowrap text-text-secondary">
                <time dateTime={post.created_at}>{formatDate(post.created_at)}</time>
              </td>
              <td className="px-3 py-2.5 text-right">
                <RoleActions post={post} />
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function RolesCards({ rows }: { rows: JobPostSummary[] }) {
  return (
    <ul className="flex flex-col gap-2 md:hidden" aria-label="Roles">
      {rows.map((post) => (
        <li key={post.id} className="rounded-lg border bg-surface p-3">
          <div className="flex items-start justify-between gap-2">
            <div className="min-w-0">
              <Link href={`/admin/roles/${post.id}`} className="block font-medium hover:underline">
                {post.title}
              </Link>
              <p className="mt-0.5 truncate text-sm text-muted-foreground">
                {[post.location, post.employment_type && employmentTypeLabel(post.employment_type)]
                  .filter(Boolean)
                  .join(" · ") || "No location set"}
              </p>
            </div>
            <RoleActions post={post} />
          </div>
          <div className="mt-3 flex items-center justify-between gap-2">
            <StatusBadge status={post.status} />
            <time dateTime={post.created_at} className="text-xs text-muted-foreground">
              {formatDate(post.created_at)}
            </time>
          </div>
          {/* A description list pairs each figure with its label for
              assistive technology, which a row of bare numbers would not. */}
          <dl className="mt-3 grid grid-cols-2 gap-2 border-t pt-3 text-sm">
            <div>
              <dt className="text-xs text-muted-foreground">Vacancies</dt>
              <dd>
                <Vacancies post={post} />
              </dd>
            </div>
            <div>
              <dt className="text-xs text-muted-foreground">Applications</dt>
              <dd className="font-mono">{formatNumber(post.application_count)}</dd>
            </div>
          </dl>
        </li>
      ))}
    </ul>
  );
}

function Vacancies({ post }: { post: JobPostSummary }) {
  const full = post.remaining_vacancies === 0;
  return (
    <span className="whitespace-nowrap">
      <span className="font-mono">{formatNumber(post.vacancies)}</span>
      <span className={`ml-1.5 text-xs ${full ? "font-medium text-text-secondary" : "text-muted-foreground"}`}>
        {full ? "all filled" : `${formatNumber(post.remaining_vacancies)} left`}
      </span>
    </span>
  );
}

function Unset() {
  return <span className="text-muted-foreground">—</span>;
}

function RoleActions({ post }: { post: JobPostSummary }) {
  const transition = useTransitionJobPost();
  const [confirmClose, setConfirmClose] = useState(false);

  function move(to: TransitionTarget) {
    transition.mutate({ id: post.id, to, title: post.title });
  }

  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger
          render={<Button variant="ghost" size="icon-sm" aria-label={`Actions for ${post.title}`} />}
        >
          <MoreHorizontalIcon aria-hidden="true" />
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" className="w-44">
          <DropdownMenuItem render={<Link href={`/admin/roles/${post.id}`} />}>
            <EyeIcon aria-hidden="true" />
            View
          </DropdownMenuItem>
          <DropdownMenuItem render={<Link href={`/admin/roles/${post.id}/edit`} />}>
            <PencilIcon aria-hidden="true" />
            Edit
          </DropdownMenuItem>
          {post.status !== "closed" && <DropdownMenuSeparator />}
          {/* Only the move the lifecycle allows from this status is offered:
              draft → open → closed, and closed is final. */}
          {post.status === "draft" && (
            <DropdownMenuItem onClick={() => move("open")}>
              <RocketIcon aria-hidden="true" />
              Open for applications
            </DropdownMenuItem>
          )}
          {post.status === "open" && (
            <DropdownMenuItem variant="destructive" onClick={() => setConfirmClose(true)}>
              <LockIcon aria-hidden="true" />
              Close role
            </DropdownMenuItem>
          )}
        </DropdownMenuContent>
      </DropdownMenu>

      <AlertDialog open={confirmClose} onOpenChange={setConfirmClose}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Close “{post.title}”?</AlertDialogTitle>
            <AlertDialogDescription>
              Candidates will no longer be able to apply. Closing is final: a closed role cannot be reopened.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Keep it open</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              onClick={() => {
                setConfirmClose(false);
                move("closed");
              }}
            >
              Close role
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}

/**
 * FirstRoleEmptyState is what a new admin sees first. It says what a role
 * is in this product and what happens after creating one, so the empty
 * screen teaches the workflow instead of only reporting an absence.
 */
function FirstRoleEmptyState() {
  return (
    <EmptyState
      icon={BriefcaseIcon}
      title="Create your first role"
      action={
        <Link href="/admin/roles/new" className={buttonVariants({ size: "lg" })}>
          <PlusIcon aria-hidden="true" />
          Create role
        </Link>
      }
    >
      <p>
        A role is a job you are hiring for. It starts as a private draft, where you write the description, build the
        application form and set the rubric that scores each answer.
      </p>
      <ol className="mx-auto mt-4 grid max-w-md gap-2 text-left text-sm sm:grid-cols-3 sm:text-center">
        <li className="rounded-md bg-surface-sunken px-3 py-2">
          <span className="font-medium text-foreground">1. Draft</span> the role and its questions
        </li>
        <li className="rounded-md bg-surface-sunken px-3 py-2">
          <span className="font-medium text-foreground">2. Open</span> it to start taking applications
        </li>
        <li className="rounded-md bg-surface-sunken px-3 py-2">
          <span className="font-medium text-foreground">3. Review</span> candidates ranked by score
        </li>
      </ol>
    </EmptyState>
  );
}

/** RolesSkeleton has the table's shape, so the page does not jump when rows arrive. */
function RolesSkeleton() {
  const widths = ["w-56", "w-44", "w-64", "w-40", "w-52", "w-48"];
  return (
    <div aria-busy="true" aria-label="Loading roles" className="flex flex-col gap-4">
      <div className="flex flex-col gap-3 md:flex-row md:justify-between">
        <Skeleton className="h-8 w-72" />
        <Skeleton className="h-8 md:w-72" />
      </div>
      <div className="hidden overflow-hidden rounded-lg border bg-surface md:block">
        <div className="flex h-9 items-center gap-6 border-b bg-surface-sunken/60 px-3">
          {["w-16", "w-12", "w-16", "ml-auto w-16", "w-20", "w-14"].map((w, i) => (
            <Skeleton key={i} className={`h-3 ${w}`} />
          ))}
        </div>
        {widths.map((w, i) => (
          <div key={i} className="flex h-[3.25rem] items-center gap-6 border-b px-3 last:border-0">
            <div className="flex w-80 flex-col gap-1.5">
              <Skeleton className={`h-3.5 ${w}`} />
              <Skeleton className="h-3 w-20" />
            </div>
            <Skeleton className="h-6 w-16 rounded-full" />
            <Skeleton className="h-3.5 w-20" />
            <Skeleton className="ml-auto h-3.5 w-16" />
            <Skeleton className="h-3.5 w-10" />
            <Skeleton className="h-3.5 w-20" />
            <Skeleton className="size-7" />
          </div>
        ))}
      </div>
      <div className="flex flex-col gap-2 md:hidden">
        {widths.slice(0, 3).map((w, i) => (
          <Skeleton key={i} className="h-36 rounded-lg" />
        ))}
      </div>
    </div>
  );
}
