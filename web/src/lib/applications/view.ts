import type { ApplicationStatus } from "@/lib/api/types";

export const applicationStatuses: { value: ApplicationStatus; label: string }[] = [
  { value: "submitted", label: "Submitted" },
  { value: "shortlisted", label: "Shortlisted" },
  { value: "interview", label: "Interview" },
  { value: "offer", label: "Offer" },
  { value: "rejected", label: "Rejected" },
];

export const PAGE_SIZE = 25;

/**
 * ApplicationsView is everything that decides what the table shows. It lives
 * in the URL, not in component state, so a filtered, sorted, searched page
 * can be bookmarked, shared, reloaded, and reached again with Back.
 */
export interface ApplicationsView {
  status?: ApplicationStatus;
  /** job_post_id */
  role?: string;
  sort: "newest" | "score";
  /** Full-text search over the answers; needs a role. */
  q: string;
  /** Keyset cursor of the current page (list mode). */
  after?: string;
  /** 1-based page number, for "Showing 26–50" only; the cursor does the paging. */
  page: number;
  /** Offset of the current page (search mode, which pages by offset). */
  offset: number;
}

const isStatus = (s: string | null): s is ApplicationStatus => applicationStatuses.some((x) => x.value === s);
const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

/**
 * parseView reads the view from the URL. Anything unrecognised falls back to
 * the default instead of being sent on: a hand-edited or stale link degrades
 * to a sensible page rather than an error.
 */
export function parseView(params: URLSearchParams): ApplicationsView {
  const status = params.get("status");
  const role = params.get("role");
  const page = Number(params.get("page"));
  const offset = Number(params.get("offset"));
  return {
    status: isStatus(status) ? status : undefined,
    role: role && UUID.test(role) ? role : undefined,
    sort: params.get("sort") === "score" ? "score" : "newest",
    q: (params.get("q") ?? "").slice(0, 200),
    after: params.get("after") || undefined,
    page: Number.isInteger(page) && page > 1 ? page : 1,
    offset: Number.isInteger(offset) && offset > 0 ? offset : 0,
  };
}

/** viewToSearch writes the view back as a query string, leaving defaults out so URLs stay short. */
export function viewToSearch(view: ApplicationsView): string {
  const params = new URLSearchParams();
  if (view.status) params.set("status", view.status);
  if (view.role) params.set("role", view.role);
  if (view.sort === "score") params.set("sort", "score");
  if (view.q.trim()) params.set("q", view.q.trim());
  if (view.after) params.set("after", view.after);
  if (view.page > 1) params.set("page", String(view.page));
  if (view.offset > 0) params.set("offset", String(view.offset));
  const s = params.toString();
  return s ? `?${s}` : "";
}

/**
 * withFilters applies a change to what is being looked at. Any such change
 * starts again from the first page: a cursor or offset belongs to the old
 * result set and would point somewhere meaningless in the new one.
 */
export function withFilters(view: ApplicationsView, change: Partial<Pick<ApplicationsView, "status" | "role" | "sort" | "q">>) {
  return { ...view, ...change, after: undefined, page: 1, offset: 0 };
}

/** isSearching reports whether the table shows full-text search results. */
export const isSearching = (view: ApplicationsView) => view.q.trim() !== "" && view.role !== undefined;
