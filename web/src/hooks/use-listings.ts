"use client";

import { useInfiniteQuery, useQuery } from "@tanstack/react-query";

import { api } from "@/lib/api/client";
import type { JobListingDetail, JobListingPage } from "@/lib/api/types";
import { queryKeys } from "@/lib/query/keys";

/**
 * useListings is the public listing, a keyset page at a time. auth: false,
 * so a visitor with a stale token in storage still sees the page instead of
 * being bounced through a refresh that will fail.
 */
export function useListings() {
  return useInfiniteQuery({
    queryKey: queryKeys.listings.list(),
    queryFn: ({ signal, pageParam }) =>
      api<JobListingPage>("/v1/job-posts", { signal, auth: false, query: { limit: 24, after: pageParam } }),
    // The first page has no cursor; each later one starts after the last.
    initialPageParam: undefined as string | undefined,
    // null from the server means "no more pages"; TanStack Query stops on undefined.
    getNextPageParam: (last) => last.next_cursor ?? undefined,
  });
}

/** useListing is one open post's public page, with its application form. */
export function useListing(id: string) {
  return useQuery({
    queryKey: queryKeys.listings.detail(id),
    queryFn: ({ signal }) => api<JobListingDetail>(`/v1/job-posts/${id}/listing`, { signal, auth: false }),
  });
}
