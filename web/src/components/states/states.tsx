"use client";

import type { UseQueryResult } from "@tanstack/react-query";
import { AlertTriangleIcon, RotateCwIcon, type LucideIcon } from "lucide-react";
import type { ReactNode } from "react";

import { Button } from "@/components/ui/button";
import { ApiError, errorMessage } from "@/lib/api/errors";

/**
 * EmptyState is shown when a request succeeded and there is nothing to show.
 * It says what would be here and offers the action that would put it there.
 */
export function EmptyState({
  icon: Icon,
  title,
  children,
  action,
}: {
  icon?: LucideIcon;
  title: string;
  children?: ReactNode;
  action?: ReactNode;
}) {
  return (
    <div className="flex flex-col items-center gap-3 rounded-lg border border-dashed bg-surface px-6 py-12 text-center">
      {Icon && (
        <span className="flex size-10 items-center justify-center rounded-full bg-surface-sunken text-muted-foreground">
          <Icon className="size-5" aria-hidden="true" />
        </span>
      )}
      <h2 className="text-lg font-medium">{title}</h2>
      {children && <div className="max-w-prose text-text-secondary">{children}</div>}
      {action && <div className="mt-2">{action}</div>}
    </div>
  );
}

/**
 * ErrorState is shown when a request failed and there is no older data to
 * fall back on. It shows the server's message, the request ID to quote, and
 * a retry button.
 */
export function ErrorState({
  error,
  title = "Something went wrong",
  onRetry,
  retrying = false,
}: {
  error: unknown;
  title?: string;
  onRetry?: () => void;
  retrying?: boolean;
}) {
  const requestId = error instanceof ApiError ? error.requestId : null;

  return (
    // role="alert" makes a screen reader announce the failure when it appears,
    // rather than leaving the user to discover it.
    <div role="alert" className="flex flex-col items-start gap-3 rounded-lg border bg-surface p-5">
      <div className="flex items-center gap-2 text-destructive">
        <AlertTriangleIcon className="size-4" aria-hidden="true" />
        <h2 className="font-medium">{title}</h2>
      </div>
      <p className="max-w-prose text-text-secondary">{errorMessage(error)}</p>
      {requestId && (
        <p className="text-xs text-muted-foreground">
          Request ID <code className="font-mono">{requestId}</code>
        </p>
      )}
      {onRetry && (
        <Button variant="outline" size="sm" onClick={onRetry} disabled={retrying}>
          <RotateCwIcon className={retrying ? "animate-spin" : undefined} aria-hidden="true" />
          {retrying ? "Retrying…" : "Try again"}
        </Button>
      )}
    </div>
  );
}

/**
 * QueryView renders the one of four states a query is in, so every page
 * handles them the same way:
 *
 *   pending, no data      → loading (a skeleton shaped like the content)
 *   failed, no data       → ErrorState with a retry
 *   succeeded, but empty  → empty
 *   has data              → children(data)
 *
 * "No data" matters: once a query has data, a failed background refresh
 * keeps showing it (the query client toasts the failure) instead of
 * replacing a working page with an error.
 */
export function QueryView<T>({
  query,
  loading,
  empty,
  isEmpty,
  errorTitle,
  children,
}: {
  query: UseQueryResult<T>;
  loading: ReactNode;
  empty?: ReactNode;
  isEmpty?: (data: T) => boolean;
  errorTitle?: string;
  children: (data: T) => ReactNode;
}) {
  if (query.data === undefined) {
    if (query.isError) {
      return (
        <ErrorState
          error={query.error}
          title={errorTitle}
          onRetry={() => void query.refetch()}
          retrying={query.isFetching}
        />
      );
    }
    return <>{loading}</>;
  }

  if (empty !== undefined && isEmpty?.(query.data)) return <>{empty}</>;
  return <>{children(query.data)}</>;
}
