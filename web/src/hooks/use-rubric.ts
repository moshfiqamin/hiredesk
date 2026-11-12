"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { api } from "@/lib/api/client";
import type { CriterionInput, ReplacedRubric, Rubric } from "@/lib/api/types";
import { queryKeys } from "@/lib/query/keys";

/** useRubric is a post's current rubric version. */
export function useRubric(jobPostId: string) {
  return useQuery({
    queryKey: queryKeys.jobPosts.rubric(jobPostId),
    queryFn: ({ signal }) => api<Rubric>(`/v1/job-posts/${jobPostId}/rubric`, { signal }),
  });
}

/** useReplaceRubric saves the whole rubric as its next version. */
export function useReplaceRubric(jobPostId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (criteria: CriterionInput[]) =>
      api<ReplacedRubric>(`/v1/job-posts/${jobPostId}/rubric`, { method: "PUT", body: { criteria } }),
    onSuccess: (rubric) => {
      queryClient.setQueryData<Rubric>(queryKeys.jobPosts.rubric(jobPostId), {
        job_post_id: rubric.job_post_id,
        version: rubric.version,
        criteria: rubric.criteria,
      });
    },
  });
}

/** useRescoreAll rescores every application to the post against the current rubric. */
export function useRescoreAll(jobPostId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: () =>
      api<{ rescored: number; changed: number; rubric_version: number }>(`/v1/job-posts/${jobPostId}/rescore-all`, {
        method: "POST",
      }),
    // Scores feed the applications list and ranking.
    onSuccess: () => queryClient.invalidateQueries({ queryKey: queryKeys.applications.all }),
  });
}
