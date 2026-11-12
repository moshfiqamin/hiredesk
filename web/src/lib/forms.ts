import type { FieldValues, Path, UseFormSetError } from "react-hook-form";

import { ApiError } from "@/lib/api/errors";

/**
 * applyServerErrors puts the per-field problems of a 422 onto the form's
 * fields with setError, so each message appears under the input it is about.
 * A problem with a field the form does not have, or any other error, goes to
 * the form-level "root.server" error instead. Returns true if the error was a
 * validation failure.
 */
export function applyServerErrors<T extends FieldValues>(
  error: unknown,
  setError: UseFormSetError<T>,
  fields: readonly string[],
): boolean {
  if (!(error instanceof ApiError) || error.code !== "validation_failed") {
    setError("root.server", { type: "server", message: error instanceof Error ? error.message : "Could not save." });
    return false;
  }

  const unmatched: string[] = [];
  let first = true;
  for (const [field, problem] of Object.entries(error.details)) {
    // The server's messages start lower-case ("must be at least 1") because
    // they follow the field name; here they stand alone under a label.
    const text = String(problem);
    const message = text.charAt(0).toUpperCase() + text.slice(1);
    if (fields.includes(field)) {
      // shouldFocus on the first one moves the keyboard to the problem, the
      // same as a client-side validation failure does.
      setError(field as Path<T>, { type: "server", message }, { shouldFocus: first });
      first = false;
    } else {
      unmatched.push(`${field} ${text}`);
    }
  }
  if (unmatched.length > 0) {
    setError("root.server", { type: "server", message: unmatched.join("; ") });
  }
  return true;
}
