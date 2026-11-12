"use client";

import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";

import { api } from "@/lib/api/client";
import { ApiError, errorMessage } from "@/lib/api/errors";
import type {
  ApplicationDetail,
  ApplicationPage,
  ApplicationStatus,
  Assignment,
  Ranking,
  ScoreBreakdown,
  SearchPage,
} from "@/lib/api/types";
import { PAGE_SIZE, isSearching, type ApplicationsView } from "@/lib/applications/view";
import { queryKeys } from "@/lib/query/keys";

/** useApplications is one keyset page of the list, for the view's filters and sort. */
export function useApplications(view: ApplicationsView) {
  const filters = { status: view.status, role: view.role, sort: view.sort, after: view.after };
  return useQuery({
    queryKey: queryKeys.applications.list(filters),
    queryFn: ({ signal }) =>
      api<ApplicationPage>("/v1/applications", {
        signal,
        query: {
          status: view.status,
          job_post_id: view.role,
          sort: view.sort === "score" ? "score" : undefined,
          limit: PAGE_SIZE,
          after: view.after,
        },
      }),
    enabled: !isSearching(view),
    // While the next page or filter loads, keep showing the current rows
    // (marked as stale) instead of dropping back to a skeleton each time.
    placeholderData: keepPreviousData,
  });
}

/** useApplicationSearch is one page of full-text matches within a role. */
export function useApplicationSearch(view: ApplicationsView) {
  const filters = { role: view.role, q: view.q.trim(), offset: view.offset };
  return useQuery({
    queryKey: queryKeys.applications.search(filters),
    queryFn: ({ signal }) =>
      api<SearchPage>(`/v1/job-posts/${view.role}/applications/search`, {
        signal,
        query: { q: view.q.trim(), limit: PAGE_SIZE, offset: view.offset },
      }),
    enabled: isSearching(view),
    placeholderData: keepPreviousData,
  });
}

export interface BulkResult {
  changed: number;
  refused: { name: string; reason: string }[];
}

/**
 * useBulkStatus moves several applications to one status. The API changes
 * one application per request, and each is judged on its own (a submitted
 * application can be shortlisted; an offer cannot), so this sends one PATCH
 * each, a few at a time, and reports which were refused and why.
 */
export function useBulkStatus() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({ rows, status }: { rows: { id: string; name: string }[]; status: ApplicationStatus }) => {
      const result: BulkResult = { changed: 0, refused: [] };
      const queue = [...rows];
      // Four workers share one queue: a bounded number of requests in flight,
      // rather than fifty at once or one at a time.
      const worker = async () => {
        for (let row = queue.shift(); row; row = queue.shift()) {
          try {
            await api(`/v1/applications/${row.id}/status`, { method: "PATCH", body: { status } });
            result.changed++;
          } catch (error) {
            result.refused.push({ name: row.name, reason: errorMessage(error) });
          }
        }
      };
      await Promise.all(Array.from({ length: Math.min(4, rows.length) }, worker));
      return result;
    },
    // Statuses changed, so every list, search and detail may be stale.
    onSettled: () => queryClient.invalidateQueries({ queryKey: queryKeys.applications.all }),
  });
}

export function useApplication(id: string) {
  return useQuery({
    queryKey: queryKeys.applications.detail(id),
    queryFn: ({ signal }) => api<ApplicationDetail>(`/v1/applications/${id}`, { signal }),
  });
}

export function useApplicationScore(id: string) {
  return useQuery({
    queryKey: queryKeys.applications.score(id),
    queryFn: ({ signal }) => api<ScoreBreakdown>(`/v1/applications/${id}/score`, { signal }),
  });
}

/** useAssignments is who is assigned to interview for the application (admin only). */
export function useAssignments(id: string) {
  return useQuery({
    queryKey: queryKeys.applications.assignments(id),
    queryFn: ({ signal }) => api<{ data: Assignment[] }>(`/v1/applications/${id}/assignments`, { signal }),
    select: (page) => page.data,
  });
}

/** useRanking is every applicant to a role, best score first. */
export function useRanking(jobPostId: string) {
  return useQuery({
    queryKey: queryKeys.jobPosts.ranking(jobPostId),
    // The cap: a whole pool fits at this project's scale, and the cut-off
    // line needs every row above it.
    queryFn: ({ signal }) => api<Ranking>(`/v1/job-posts/${jobPostId}/ranking`, { signal, query: { limit: 500 } }),
  });
}

/** statusRefusal turns a refused move into a sentence, using the details the server sent. */
function statusRefusal(error: unknown): string {
  if (error instanceof ApiError && error.code === "no_vacancies") {
    return `Every vacancy already has an offer (${String(error.details.offers_made)} of ${String(error.details.vacancies)}).`;
  }
  return errorMessage(error);
}

/**
 * useUpdateStatus moves one application to a new status. Not optimistic: the
 * server can refuse an offer when the vacancies are full, and a status shown
 * then taken back is worse than a short wait on a deliberate action.
 */
export function useUpdateStatus(id: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (status: ApplicationStatus) =>
      api(`/v1/applications/${id}/status`, { method: "PATCH", body: { status } }),
    onSuccess: (_data, status) => toast.success(`Moved to ${status}`),
    onError: (error) => toast.error("Could not change the status", { description: statusRefusal(error) }),
    // The detail carries the new allowed moves and a new timeline entry; the
    // lists and rankings show the status too.
    onSettled: () =>
      Promise.all([
        queryClient.invalidateQueries({ queryKey: queryKeys.applications.all }),
        queryClient.invalidateQueries({ queryKey: queryKeys.jobPosts.all }),
      ]),
  });
}
