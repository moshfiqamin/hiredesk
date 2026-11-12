"use client";

import { PencilIcon } from "lucide-react";
import { useId, useState, type FormEvent } from "react";
import { cn } from "cn";

import { Button } from "@/components/ui/button";
import { FieldDescription, FieldError, FieldLabel } from "@/components/ui/field";
import { Textarea } from "@/components/ui/textarea";
import { useSaveReview } from "@/hooks/use-reviews";
import { errorMessage, fieldErrors } from "@/lib/api/errors";
import type { Review } from "@/lib/api/types";
import { formatDate } from "@/lib/format";
import { bandFor, confidence, markBands, MAX_NOTES } from "@/lib/reviews/scale";

/**
 * ReviewPanel is the interviewer's own review of one application: the form
 * before they have filed one, their review with an Edit button afterwards.
 * It never shows, or receives, anyone else's review: the API sends an
 * interviewer only their own (docs/notes/09-2).
 */
export function ReviewPanel({ applicationId, review }: { applicationId: string; review: Review | undefined }) {
  const [editing, setEditing] = useState(false);

  if (review && !editing) {
    const band = bandFor(review.mark);
    return (
      <section aria-labelledby="my-review-heading" className="flex flex-col gap-3 rounded-xl border bg-surface p-5">
        <div className="flex items-center justify-between gap-3">
          <h2 id="my-review-heading" className="font-semibold">
            Your review
          </h2>
          <Button variant="outline" size="sm" onClick={() => setEditing(true)}>
            <PencilIcon aria-hidden="true" />
            Edit
          </Button>
        </div>
        <p className="flex items-baseline gap-2">
          <span className="font-mono text-3xl font-semibold tabular-nums">{review.mark}</span>
          <span className="text-sm text-text-secondary">out of 10 · {band.label}</span>
        </p>
        <p className="text-sm text-text-secondary">{band.meaning}</p>
        {review.notes ? (
          <p className="text-sm break-words whitespace-pre-line">{review.notes}</p>
        ) : (
          <p className="text-sm text-muted-foreground">No notes.</p>
        )}
        <p className="text-xs text-muted-foreground">
          {review.updated_at !== review.created_at ? "Edited " : "Submitted "}
          <time dateTime={review.updated_at}>{formatDate(review.updated_at)}</time>
        </p>
      </section>
    );
  }

  return <ReviewForm applicationId={applicationId} review={review} onDone={() => setEditing(false)} />;
}

function ReviewForm({ applicationId, review, onDone }: { applicationId: string; review: Review | undefined; onDone: () => void }) {
  const save = useSaveReview(applicationId, review);
  const [mark, setMark] = useState<number | null>(review?.mark ?? null);
  const [notes, setNotes] = useState(review?.notes ?? "");
  const [tried, setTried] = useState(false);
  const ids = useId();

  // Characters as the server counts them: code points, not UTF-16 units.
  const length = [...notes].length;
  const tooLong = length > MAX_NOTES;
  const serverErrors = fieldErrors(save.error);
  const markError = (tried && mark === null ? "Choose a mark from 1 to 10." : null) ?? serverErrors.mark;
  const notesError = tooLong ? `Notes can be at most ${MAX_NOTES.toLocaleString("en-GB")} characters.` : serverErrors.notes;

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    setTried(true);
    if (mark === null || tooLong) return;
    save.mutate({ mark, notes }, { onSuccess: onDone });
  }

  const selected = mark === null ? null : bandFor(mark);

  return (
    <form onSubmit={onSubmit} noValidate aria-labelledby={`${ids}-title`} className="flex flex-col gap-5 rounded-xl border bg-surface p-5">
      <div className="flex flex-col gap-1">
        <h2 id={`${ids}-title`} className="font-semibold">
          {review ? "Edit your review" : "Your review"}
        </h2>
        <p className="text-sm text-text-secondary">
          Mark against the bar for this role, not against other candidates. Other interviewers&apos; marks are hidden from
          you, and yours from them, so each judgement stays independent.
        </p>
      </div>

      <fieldset aria-describedby={[`${ids}-scale-hint`, markError ? `${ids}-mark-error` : undefined].filter(Boolean).join(" ")} className="flex flex-col gap-3">
        <legend className="mb-1 text-sm font-medium">Mark</legend>
        <p id={`${ids}-scale-hint`} className="text-sm text-text-secondary">
          Choose the band first. Within it, take the higher mark if you would defend it without hesitation.
        </p>
        {/* One radio group of ten, so arrow keys run 1 → 10 across the bands. */}
        <div className="grid gap-2">
          {markBands.map((band) => (
            <div key={band.label} className="grid grid-cols-[1fr_auto] items-center gap-3 rounded-lg border p-2 pl-3 has-checked:border-primary has-checked:bg-primary-soft/50">
              <div className="min-w-0">
                <p className="text-sm font-medium">{band.label}</p>
                <p id={`${ids}-band-${band.marks[0]}`} className="text-xs text-text-secondary">
                  {band.meaning}
                </p>
              </div>
              <div className="flex gap-1.5">
                {band.marks.map((m) => (
                  <label
                    key={m}
                    className={cn(
                      "relative flex size-10 cursor-pointer items-center justify-center rounded-md border border-border-field bg-surface font-mono text-sm font-medium tabular-nums",
                      "has-checked:border-primary has-checked:bg-primary has-checked:text-primary-foreground has-focus-visible:ring-2 has-focus-visible:ring-ring has-focus-visible:ring-offset-2 has-focus-visible:ring-offset-surface",
                    )}
                  >
                    <input
                      type="radio"
                      name={`${ids}-mark`}
                      value={m}
                      checked={mark === m}
                      onChange={() => setMark(m)}
                      // Transparent and laid over the whole button: still
                      // focusable and announced, and the click target is the
                      // full 40px square rather than a clipped 1px box.
                      className="absolute inset-0 size-full cursor-pointer appearance-none opacity-0"
                      aria-describedby={`${ids}-band-${band.marks[0]}`}
                    />
                    <span aria-hidden="true">{m}</span>
                    <span className="sr-only">
                      {m}, {band.label}, {confidence(m)}
                    </span>
                  </label>
                ))}
              </div>
            </div>
          ))}
        </div>
        {selected && mark !== null && (
          <p className="text-sm" aria-live="polite">
            <span className="font-medium">
              {mark} · {selected.label}
            </span>{" "}
            <span className="text-text-secondary">({confidence(mark)})</span>
          </p>
        )}
        {markError && <FieldError id={`${ids}-mark-error`}>{markError}</FieldError>}
      </fieldset>

      <div className="flex flex-col gap-2">
        <FieldLabel htmlFor={`${ids}-notes`}>Notes</FieldLabel>
        <FieldDescription id={`${ids}-notes-hint`}>
          Optional. What you saw that led to the mark: specific answers or moments, so another reader can follow your
          reasoning.
        </FieldDescription>
        <Textarea
          id={`${ids}-notes`}
          rows={6}
          value={notes}
          onChange={(e) => setNotes(e.target.value)}
          aria-invalid={Boolean(notesError) || undefined}
          aria-describedby={[`${ids}-notes-hint`, `${ids}-notes-count`, notesError ? `${ids}-notes-error` : undefined].filter(Boolean).join(" ")}
        />
        <p id={`${ids}-notes-count`} className={cn("text-right font-mono text-xs tabular-nums", tooLong ? "text-destructive" : "text-muted-foreground")}>
          {length.toLocaleString("en-GB")} / {MAX_NOTES.toLocaleString("en-GB")} characters
        </p>
        {notesError && <FieldError id={`${ids}-notes-error`}>{notesError}</FieldError>}
      </div>

      {save.isError && !serverErrors.mark && !serverErrors.notes && (
        <p role="alert" className="text-sm text-destructive">
          {errorMessage(save.error)}
        </p>
      )}

      <div className="flex flex-wrap gap-2">
        <Button type="submit" size="lg" disabled={save.isPending}>
          {save.isPending ? "Saving…" : review ? "Save changes" : "Submit review"}
        </Button>
        {review && (
          <Button type="button" variant="ghost" size="lg" onClick={onDone} disabled={save.isPending}>
            Cancel
          </Button>
        )}
      </div>
    </form>
  );
}
