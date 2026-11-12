/**
 * API_URL is the origin of the HireDesk API, with no trailing slash.
 *
 * NEXT_PUBLIC_ values are replaced with literals by `next build`, so this is
 * fixed when the bundle is built, not when the page loads. It is read here
 * and nowhere else, as the Go side reads configuration only in
 * internal/config.
 */
export const API_URL = (process.env.NEXT_PUBLIC_API_URL || "http://localhost:8080").replace(/\/+$/, "");
