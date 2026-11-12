"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { api } from "@/lib/api/client";
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

/** useJobPost is one post with its application form. */
export function useJobPost(id: string) {
  return useQuery({
    queryKey: queryKeys.jobPosts.detail(id),
    queryFn: ({ signal }) => api<JobPost>(`/v1/job-posts/${id}`, { signal }),
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
