/**
 * Query keys: the one place every cache key is built.
 *
 * The convention, followed by every hook:
 *
 *   [resource]                          everything about a resource
 *   [resource, "list"]                  every list of it
 *   [resource, "list", filters]         one list; filters is an object, last
 *   [resource, "detail", id]            one record
 *   [resource, "detail", id, part]      something that belongs to one record
 *
 * Examples:
 *
 *   ["jobPosts"]                                  all job-post data
 *   ["jobPosts", "list"]                          the admin roles list
 *   ["jobPosts", "detail", id]                    one post
 *   ["jobPosts", "detail", id, "questions"]       its application form
 *   ["applications", "list", { status, sort }]    one filtered page set
 *
 * Why this shape:
 *
 * - Keys are matched by prefix. Invalidating ["jobPosts", "list"] refreshes
 *   every list and leaves cached details alone; invalidating
 *   ["jobPosts", "detail", id] refreshes that post and everything nested
 *   under it.
 * - The "list" / "detail" segment stops an ID from ever colliding with a
 *   list key, which ["jobPosts", id] next to ["jobPosts", "drafts"] could.
 * - Filters go last, as one object. TanStack Query hashes objects with their
 *   keys sorted, so { a, b } and { b, a } are the same cache entry.
 *
 * Nothing outside this file writes a key by hand.
 */
export const queryKeys = {
  jobPosts: {
    all: ["jobPosts"] as const,
    lists: () => [...queryKeys.jobPosts.all, "list"] as const,
    detail: (id: string) => [...queryKeys.jobPosts.all, "detail", id] as const,
    // Nested under the post, so invalidating the post refreshes these too.
    questions: (id: string) => [...queryKeys.jobPosts.detail(id), "questions"] as const,
    rubric: (id: string) => [...queryKeys.jobPosts.detail(id), "rubric"] as const,
    ranking: (id: string) => [...queryKeys.jobPosts.detail(id), "ranking"] as const,
  },
  applications: {
    all: ["applications"] as const,
    lists: () => [...queryKeys.applications.all, "list"] as const,
    // The filters object is the whole view: status, role, sort and the page.
    list: (filters: Record<string, string | number | undefined>) =>
      [...queryKeys.applications.lists(), filters] as const,
    search: (filters: Record<string, string | number | undefined>) =>
      [...queryKeys.applications.lists(), "search", filters] as const,
    detail: (id: string) => [...queryKeys.applications.all, "detail", id] as const,
    score: (id: string) => [...queryKeys.applications.detail(id), "score"] as const,
    assignments: (id: string) => [...queryKeys.applications.detail(id), "assignments"] as const,
  },
  // The staff directory, for the assignment picker. Assigning changes the
  // loads in it, so assignment mutations invalidate it.
  interviewers: ["interviewers"] as const,
  // The public pages. Separate from jobPosts: a different endpoint, a
  // different shape, and readable without a session.
  listings: {
    all: ["listings"] as const,
    list: () => [...queryKeys.listings.all, "list"] as const,
    detail: (id: string) => [...queryKeys.listings.all, "detail", id] as const,
  },
} as const;
