"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";

import { api } from "@/lib/api/client";
import { ApiError, errorMessage } from "@/lib/api/errors";
import type { JobPost, JobPostInput, JobPostSummary } from "@/lib/api/types";
import { queryKeys } from "@/lib/query/keys";

/** useJobPosts is the admin roles list: every post, drafts included. */
export function useJobPosts() {
  return useQuery({
    queryKey: queryKeys.jobPosts.lists(),
    // signal lets TanStack Query abort the request if the component unmounts
    // or the key changes before it answers.
    queryFn: ({ signal }) =>
      api<{ data: JobPostSummary[] }>("/v1/admin/job-posts", { signal }).then((body) => body.data),
  });
}

/**
 * useJobPost is one post with its application form. enabled lets a caller
 * wait until it knows the ID, such as the post an application belongs to.
 */
export function useJobPost(id: string, { enabled = true }: { enabled?: boolean } = {}) {
  return useQuery({
    queryKey: queryKeys.jobPosts.detail(id),
    queryFn: ({ signal }) => api<JobPost>(`/v1/job-posts/${id}`, { signal }),
    enabled,
  });
}

/** useCreateJobPost creates a draft post. */
export function useCreateJobPost() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (input: JobPostInput) => api<JobPost>("/v1/job-posts", { method: "POST", body: input }),
    onSuccess: (post) => {
      // The response is the whole post, so its detail entry is written
      // directly: opening it next costs no request.
      queryClient.setQueryData(queryKeys.jobPosts.detail(post.id), post);

      // Every list is now missing a row. Invalidating marks them stale and
      // refetches the ones on screen; the rest refetch when next used.
      return queryClient.invalidateQueries({ queryKey: queryKeys.jobPosts.lists() });
    },
  });
}

/** useUpdateJobPost changes the fields given, and only those. */
export function useUpdateJobPost(id: string) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (input: JobPostInput) => api<JobPost>(`/v1/job-posts/${id}`, { method: "PATCH", body: input }),
    onSuccess: (post) => {
      queryClient.setQueryData(queryKeys.jobPosts.detail(id), post);
      return queryClient.invalidateQueries({ queryKey: queryKeys.jobPosts.lists() });
    },
  });
}

/** The status a post can be moved to from the roles list. */
export type TransitionTarget = "open" | "closed";

/** transitionError turns a refused open or close into one readable sentence. */
function transitionError(error: unknown): string {
  // not_ready names what is missing per field, e.g. { questions: "...",
  // rubric: "..." }; those sentences are more useful than the summary.
  if (error instanceof ApiError && error.code === "not_ready") {
    const reasons = Object.values(error.details).map(String);
    if (reasons.length > 0) return reasons.join(" ");
  }
  return errorMessage(error);
}

/**
 * useTransitionJobPost opens or closes a post, optimistically: the new status
 * shows at once, and is put back if the server refuses.
 */
export function useTransitionJobPost() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: ({ id, to }: { id: string; to: TransitionTarget; title: string }) =>
      api<JobPost>(`/v1/job-posts/${id}/${to === "open" ? "open" : "close"}`, { method: "POST" }),

    // Runs before mutationFn. Whatever it returns arrives as `context` in
    // onError and onSettled, which is how the rollback finds the snapshot.
    onMutate: async ({ id, to }) => {
      const listKey = queryKeys.jobPosts.lists();
      const detailKey = queryKeys.jobPosts.detail(id);

      // A refetch already in flight could land after the optimistic write and
      // overwrite it with the old status, so in-flight reads are cancelled.
      await queryClient.cancelQueries({ queryKey: listKey });
      await queryClient.cancelQueries({ queryKey: detailKey });

      // The snapshot: the cache exactly as it was before this change.
      const previousList = queryClient.getQueryData<JobPostSummary[]>(listKey);
      const previousDetail = queryClient.getQueryData<JobPost>(detailKey);

      // The updater receives the cached value and must return a new one
      // without mutating it; React compares by reference to decide what to
      // re-render.
      queryClient.setQueryData<JobPostSummary[]>(listKey, (old) =>
        old?.map((post) => (post.id === id ? { ...post, status: to } : post)),
      );
      queryClient.setQueryData<JobPost>(detailKey, (old) => (old ? { ...old, status: to } : old));

      return { previousList, previousDetail };
    },

    onError: (error, { id, to, title }, context) => {
      // Rollback: put the snapshot back.
      queryClient.setQueryData(queryKeys.jobPosts.lists(), context?.previousList);
      queryClient.setQueryData(queryKeys.jobPosts.detail(id), context?.previousDetail);
      toast.error(`Could not ${to === "open" ? "open" : "close"} “${title}”`, {
        description: transitionError(error),
      });
    },

    onSuccess: (_post, { to, title }) => {
      toast.success(to === "open" ? `“${title}” is open for applications` : `“${title}” is closed`);
    },

    // Success or failure, the server's copy is the truth: refetch it. This
    // also picks up anything the optimistic write could not know, such as
    // updated_at.
    onSettled: (_data, _error, { id }) =>
      Promise.all([
        queryClient.invalidateQueries({ queryKey: queryKeys.jobPosts.lists() }),
        queryClient.invalidateQueries({ queryKey: queryKeys.jobPosts.detail(id) }),
      ]),
  });
}
