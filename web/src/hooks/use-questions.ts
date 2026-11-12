"use client";

import { useQuery } from "@tanstack/react-query";

import { api } from "@/lib/api/client";
import type { Question } from "@/lib/api/types";
import { queryKeys } from "@/lib/query/keys";

/** useQuestions is one post's application form, in display order. */
export function useQuestions(jobPostId: string) {
  return useQuery({
    queryKey: queryKeys.jobPosts.questions(jobPostId),
    queryFn: ({ signal }) =>
      api<{ data: Question[] }>(`/v1/job-posts/${jobPostId}/questions`, { signal }).then((body) => body.data),
  });
}
