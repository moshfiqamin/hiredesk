/**
 * ApiError is every failure the API client reports: an error envelope from
 * the server, or a request that never reached it (status 0).
 */
export class ApiError extends Error {
  /** HTTP status, or 0 when the request failed before a response arrived. */
  readonly status: number;
  /** The envelope's stable code. Branch on this, never on message. */
  readonly code: string;
  /** Per-field problems for a 422; other details for other codes. */
  readonly details: Record<string, unknown>;
  /** The server's ID for this request, to quote in a bug report. */
  readonly requestId: string | null;

  constructor(init: {
    status: number;
    code: string;
    message: string;
    details?: Record<string, unknown>;
    requestId?: string | null;
  }) {
    super(init.message);
    this.name = "ApiError";
    this.status = init.status;
    this.code = init.code;
    this.details = init.details ?? {};
    this.requestId = init.requestId ?? null;
  }

  /** True for 4xx: the request itself was wrong and repeating it will not help. */
  get isClientError(): boolean {
    return this.status >= 400 && this.status < 500;
  }
}

/**
 * fieldErrors returns the per-field messages of a validation_failed error,
 * keyed by the API's field name, or an empty object for any other error.
 */
export function fieldErrors(error: unknown): Record<string, string> {
  if (!(error instanceof ApiError) || error.code !== "validation_failed") return {};

  const out: Record<string, string> = {};
  for (const [field, problem] of Object.entries(error.details)) {
    // The server sends a string per field; a list is joined defensively.
    out[field] = Array.isArray(problem) ? problem.join("; ") : String(problem);
  }
  return out;
}

/** errorMessage returns text fit to show a person for any thrown value. */
export function errorMessage(error: unknown): string {
  if (error instanceof Error) return error.message;
  return "Something went wrong.";
}
