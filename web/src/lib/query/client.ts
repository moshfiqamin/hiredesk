import { QueryCache, QueryClient } from "@tanstack/react-query";
import { toast } from "sonner";

import { ApiError, errorMessage } from "@/lib/api/errors";

/** How many times a failed query is retried before its error is shown. */
const MAX_RETRIES = 2;

/**
 * shouldRetry decides whether a failed query is worth repeating.
 *
 * A 4xx means the request itself was refused: a 404 stays a 404, and a 422
 * stays a 422 however often it is sent. Retrying only delays the error the
 * user needs to see. 408 and 429 are the exceptions, because they say "not
 * now" rather than "not this". 5xx and network failures may be passing.
 */
export function shouldRetry(failureCount: number, error: unknown): boolean {
  if (error instanceof ApiError && error.isClientError && error.status !== 408 && error.status !== 429) {
    return false;
  }
  return failureCount < MAX_RETRIES;
}

/** makeQueryClient builds the client with HireDesk's defaults. */
export function makeQueryClient(): QueryClient {
  return new QueryClient({
    queryCache: new QueryCache({
      // A failed first load is shown inline by the page (ErrorState). A failed
      // background refresh, when the page already shows data, would otherwise
      // be silent, so it gets a toast and the old data stays on screen.
      onError: (error, query) => {
        if (query.state.data !== undefined) {
          toast.error("Could not refresh", { description: errorMessage(error) });
        }
      },
    }),
    defaultOptions: {
      queries: {
        // Data counts as fresh for 30 seconds. Moving between pages inside
        // that window reuses the cache with no request; after it, the next
        // use refetches in the background while the cached copy is shown.
        staleTime: 30_000,

        // Unused data is kept for 5 minutes (the default) after its last
        // component unmounts, so going back to a page is instant.
        gcTime: 5 * 60_000,

        retry: shouldRetry,

        // Kept on, deliberately. An admin switches between this tab and email
        // all day, and colleagues move candidates meanwhile. Returning to the
        // tab refetches only queries older than staleTime, so it costs at most
        // one request per visible query, and stops a stale pipeline being
        // acted on.
        refetchOnWindowFocus: true,
      },
      mutations: {
        // Never retried automatically. A write that timed out may have
        // succeeded; repeating it could, for example, append a second rescore
        // event. The user decides whether to try again.
        retry: false,
      },
    },
  });
}
