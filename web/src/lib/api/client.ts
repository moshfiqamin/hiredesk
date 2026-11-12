import { getSession, sessionFromTokens, setSession } from "@/lib/auth/session";
import { API_URL } from "@/lib/config";

import { ApiError } from "./errors";
import type { TokenPair } from "./types";

type Method = "GET" | "POST" | "PUT" | "PATCH" | "DELETE";

interface RequestOptions {
  method?: Method;
  /** Serialised as JSON. */
  body?: unknown;
  /** Query parameters; undefined and "" values are left out. */
  query?: Record<string, string | number | undefined>;
  /** Passed to fetch so TanStack Query can cancel a request nobody needs. */
  signal?: AbortSignal;
  /** Send the access token. True by default. */
  auth?: boolean;
}

function buildUrl(path: string, query?: RequestOptions["query"]): string {
  const url = new URL(API_URL + path);
  for (const [key, value] of Object.entries(query ?? {})) {
    if (value !== undefined && value !== "") url.searchParams.set(key, String(value));
  }
  return url.toString();
}

/** toApiError reads the error envelope from a failed response. */
async function toApiError(res: Response): Promise<ApiError> {
  const requestId = res.headers.get("X-Request-ID");
  try {
    const body = (await res.json()) as {
      error?: { code?: string; message?: string; details?: Record<string, unknown>; request_id?: string };
    };
    if (body.error?.code) {
      return new ApiError({
        status: res.status,
        code: body.error.code,
        message: body.error.message ?? res.statusText,
        details: body.error.details,
        requestId: body.error.request_id ?? requestId,
      });
    }
  } catch {
    // Not JSON: a proxy's HTML error page, say. Fall through.
  }
  return new ApiError({ status: res.status, code: "unexpected_response", message: `The API answered ${res.status}.`, requestId });
}

async function send(path: string, options: RequestOptions, accessToken: string | null): Promise<Response> {
  const headers: Record<string, string> = { Accept: "application/json" };
  if (options.body !== undefined) headers["Content-Type"] = "application/json";
  if (accessToken) headers.Authorization = `Bearer ${accessToken}`;

  try {
    return await fetch(buildUrl(path, options.query), {
      method: options.method ?? "GET",
      headers,
      body: options.body === undefined ? undefined : JSON.stringify(options.body),
      signal: options.signal,
    });
  } catch (cause) {
    // An abort is not a failure to report: rethrow it untouched so TanStack
    // Query recognises its own cancellation.
    if (cause instanceof DOMException && cause.name === "AbortError") throw cause;
    throw new ApiError({
      status: 0,
      code: "network_error",
      message: "Could not reach the HireDesk API. Check your connection, or whether the API is running.",
    });
  }
}

// The refresh in progress, if any. Several queries can hit an expired token
// at the same moment; they all wait on this one call instead of each
// spending the refresh token, which the server rotates on every use and
// treats a second use of as theft.
let refreshing: Promise<boolean> | null = null;

function refreshSession(): Promise<boolean> {
  refreshing ??= (async () => {
    const session = getSession();
    if (!session) return false;
    const res = await send("/v1/auth/refresh", { method: "POST", body: { refresh_token: session.refreshToken } }, null);
    if (!res.ok) return false;
    setSession(sessionFromTokens((await res.json()) as TokenPair));
    return true;
  })().finally(() => {
    refreshing = null;
  });
  return refreshing;
}

/**
 * api sends one request to the HireDesk API and returns the decoded body.
 *
 * A non-2xx answer is thrown as an ApiError. An expired access token is
 * refreshed once and the request repeated; if that fails the session is
 * cleared, which sends the admin shell to the login page.
 */
export async function api<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const useAuth = options.auth ?? true;

  let res = await send(path, options, useAuth ? (getSession()?.accessToken ?? null) : null);

  if (res.status === 401 && useAuth) {
    const error = await toApiError(res);
    if (error.code === "token_expired" && (await refreshSession())) {
      res = await send(path, options, getSession()?.accessToken ?? null);
    } else {
      setSession(null);
      throw error;
    }
  }

  if (!res.ok) throw await toApiError(res);

  // 204 No Content has no body to parse.
  if (res.status === 204) return undefined as T;
  return (await res.json()) as T;
}
