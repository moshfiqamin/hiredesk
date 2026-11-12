"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";

import { api } from "@/lib/api/client";
import { errorMessage } from "@/lib/api/errors";
import type { ApplicationPage, InterviewerLoad, Review } from "@/lib/api/types";
import { queryKeys } from "@/lib/query/keys";

/** useMyAssignments is every application the signed-in interviewer is assigned to; the API scopes it in SQL. */
export function useMyAssignments() {
  return useQuery({
    queryKey: queryKeys.applications.list({ mine: "interviewer" }),
    queryFn: ({ signal }) => api<ApplicationPage>("/v1/applications", { signal, query: { limit: 100 } }),
  });
}

/** useInterviewers is the staff directory with each interviewer's load (admin only). */
export function useInterviewers() {
  return useQuery({
    queryKey: queryKeys.interviewers,
    queryFn: ({ signal }) => api<{ data: InterviewerLoad[] }>("/v1/admin/interviewers", { signal }),
    select: (page) => page.data,
  });
}

/**
 * useAssignment assigns or removes one interviewer. Both change three views
 * at once: the application's assignments and timeline, the directory's
 * loads, and the lists that show assignees, so all are refreshed.
 */
export function useAssignment(applicationId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ interviewerId, assign }: { interviewerId: string; name: string; assign: boolean }) =>
      assign
        ? api(`/v1/applications/${applicationId}/assign`, { method: "POST", body: { interviewer_id: interviewerId } })
        : api(`/v1/applications/${applicationId}/assign/${interviewerId}`, { method: "DELETE" }),
    onSuccess: (_data, { name, assign }) => toast.success(assign ? `${name} assigned` : `${name} removed`),
    onError: (error, { name, assign }) =>
      toast.error(assign ? `Could not assign ${name}` : `Could not remove ${name}`, { description: errorMessage(error) }),
    onSettled: () =>
      Promise.all([
        queryClient.invalidateQueries({ queryKey: queryKeys.applications.all }),
        queryClient.invalidateQueries({ queryKey: queryKeys.interviewers }),
      ]),
  });
}

export interface ReviewInput {
  mark: number;
  notes: string;
}

/**
 * useSaveReview files the interviewer's review, or corrects it when one
 * exists. "notes": "" on a correction clears the notes, as the API
 * documents; on a first review empty notes are simply left out.
 */
export function useSaveReview(applicationId: string, existing: Review | undefined) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ mark, notes }: ReviewInput) =>
      existing
        ? api<Review>(`/v1/applications/${applicationId}/reviews/mine`, { method: "PATCH", body: { mark, notes: notes.trim() } })
        : api<Review>(`/v1/applications/${applicationId}/reviews`, {
            method: "POST",
            body: notes.trim() ? { mark, notes: notes.trim() } : { mark },
          }),
    onSuccess: () => toast.success(existing ? "Review updated" : "Review submitted"),
    // The detail holds the review; the worklist holds "reviewed by me".
    onSettled: () => queryClient.invalidateQueries({ queryKey: queryKeys.applications.all }),
  });
}
