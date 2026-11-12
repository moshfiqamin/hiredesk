"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { ArrowLeftIcon, CheckIcon, InfoIcon } from "lucide-react";
import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import { useEffect, useMemo, useRef, useState } from "react";
import { Controller, useForm, useWatch, type Control } from "react-hook-form";
import { toast } from "sonner";

import { ErrorState } from "@/components/states/states";
import { Button } from "@/components/ui/button";
import { Field, FieldDescription, FieldError, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { useListing } from "@/hooks/use-listings";
import { api } from "@/lib/api/client";
import { ApiError, errorMessage } from "@/lib/api/errors";
import type { JobListingDetail, Question } from "@/lib/api/types";
import {
  applicationSchema,
  emptyValues,
  submissionFieldErrors,
  toSubmission,
  type ApplicationValues,
} from "@/lib/applications/application-form";
import { clearDraft, loadDraft, saveDraft, type RestoredDraft } from "@/lib/applications/draft";
import { getSession } from "@/lib/auth/session";
import { queryKeys } from "@/lib/query/keys";
import { describeQuestion } from "@/lib/questions/describe";

import { isGroupKind, questionFields } from "./question-fields";

/** ApplicationForm loads the role's questions, then renders the form for them. */
export function ApplicationForm() {
  const { id } = useParams<{ id: string }>();
  const listing = useListing(id);

  if (listing.isError) {
    return <ErrorState error={listing.error} title="Could not load this role's application form" onRetry={() => void listing.refetch()} />;
  }
  if (!listing.data) {
    return (
      <div className="flex flex-col gap-4" aria-busy="true" aria-label="Loading the application form">
        <Skeleton className="h-10 w-2/3" />
        <Skeleton className="h-64" />
      </div>
    );
  }
  // RoleGate has already required a candidate session.
  const userId = getSession()!.user.id;
  // Keyed by role so the form, and its draft, start afresh for another role.
  return <ApplicationFormBody key={listing.data.id} role={listing.data} userId={userId} />;
}

function ApplicationFormBody({ role, userId }: { role: JobListingDetail; userId: string }) {
  const router = useRouter();
  const queryClient = useQueryClient();
  const questions = role.questions;

  // Read once, on mount: a lazy useState initialiser runs a single time, so
  // the draft is not reloaded (and edits lost) on every render.
  const [restored] = useState<RestoredDraft | null>(() => loadDraft(userId, role.id, questions));
  const schema = useMemo(() => applicationSchema(questions), [questions]);

  const form = useForm<ApplicationValues>({
    resolver: zodResolver(schema),
    defaultValues: restored?.values ?? emptyValues(questions),
    // Check a field when it is left, then on every change once it has an
    // error, so a message disappears the moment it is fixed but nobody is
    // scolded halfway through their first sentence.
    mode: "onTouched",
  });

  const submit = useMutation({
    mutationFn: (values: ApplicationValues) =>
      api(`/v1/job-posts/${role.id}/applications`, { method: "POST", body: toSubmission(values, questions) }),
    onSuccess: () => {
      clearDraft(userId, role.id);
      void queryClient.invalidateQueries({ queryKey: queryKeys.applications.all });
      toast.success(`Application sent for ${role.title}`);
      router.push("/my-applications");
    },
    onError: (error) => {
      // A 422 is mapped onto the fields it names, and focus moves to the
      // first, as client-side validation would have done.
      const fieldErrs = submissionFieldErrors(error, questions);
      const paths = Object.keys(fieldErrs);
      paths.forEach((path, i) =>
        form.setError(path as "cv_url" | `answers.${string}`, { type: "server", message: fieldErrs[path] }, { shouldFocus: i === 0 }),
      );
    },
  });

  const alreadyApplied = submit.error instanceof ApiError && submit.error.code === "already_applied";
  const fieldErrorCount = Object.keys(submissionFieldErrors(submit.error, questions)).length;
  const generalError = submit.isError && fieldErrorCount === 0 && !alreadyApplied ? errorMessage(submit.error) : null;

  return (
    <div className="mx-auto flex max-w-2xl flex-col gap-8">
      <header className="flex flex-col gap-3">
        <Link href={`/roles/${role.id}`} className="inline-flex w-fit items-center gap-1 rounded-sm text-sm text-muted-foreground outline-none hover:underline focus-visible:ring-2 focus-visible:ring-ring">
          <ArrowLeftIcon className="size-3.5" aria-hidden="true" />
          Back to the role
        </Link>
        <h1 className="text-2xl font-semibold tracking-tight text-balance">Apply for {role.title}</h1>
        <p className="text-text-secondary">
          Questions marked <span aria-hidden="true">*</span>
          <span className="sr-only">as required</span> are required. Your answers are saved in this browser as you type,
          so you can close the tab and come back.
        </p>
      </header>

      {restored && <RestoredNotice restored={restored} />}

      <form onSubmit={form.handleSubmit((values) => submit.mutate(values))} noValidate className="flex flex-col gap-8">
        <Controller
          control={form.control}
          name="cv_url"
          render={({ field, fieldState }) => (
            <Field data-invalid={fieldState.invalid || undefined}>
              <FieldLabel htmlFor="cv_url">
                Link to your CV <span aria-hidden="true">*</span>
              </FieldLabel>
              <Input
                id="cv_url"
                type="url"
                inputMode="url"
                autoComplete="url"
                placeholder="https://"
                required
                {...field}
                aria-invalid={fieldState.invalid || undefined}
                aria-describedby={fieldState.error ? "cv_url-hint cv_url-error" : "cv_url-hint"}
              />
              <FieldDescription id="cv_url-hint">A shareable link to a PDF or document, such as Google Drive or Dropbox.</FieldDescription>
              {fieldState.error && <FieldError id="cv_url-error">{fieldState.error.message}</FieldError>}
            </Field>
          )}
        />

        <ol className="flex flex-col gap-8">
          {questions.map((q, i) => (
            <li key={q.id}>
              <QuestionInput question={q} index={i} control={form.control} />
            </li>
          ))}
        </ol>

        {alreadyApplied && (
          <p role="alert" className="rounded-md border bg-surface p-3 text-sm">
            You have already applied for this role.{" "}
            <Link href="/my-applications" className="font-medium underline-offset-2 hover:underline">
              See your applications
            </Link>
          </p>
        )}
        {generalError && (
          <p role="alert" className="text-sm text-destructive">
            {generalError}
          </p>
        )}

        <div className="flex flex-col-reverse gap-3 border-t pt-6 sm:flex-row sm:items-center sm:justify-between">
          <DraftStatus control={form.control} userId={userId} postId={role.id} questions={questions} />
          <Button type="submit" size="lg" disabled={submit.isPending}>
            {submit.isPending ? "Sending…" : "Send application"}
          </Button>
        </div>
      </form>
    </div>
  );
}

/** QuestionInput is one question: its label, hint, the input for its kind, and its error. */
function QuestionInput({ question: q, index, control }: { question: Question; index: number; control: Control<ApplicationValues> }) {
  const Input = questionFields[q.kind];
  const id = `q-${q.id}`;
  const label = (
    <>
      <span className="font-mono text-muted-foreground tabular-nums">{index + 1}.</span>{" "}
      {q.prompt}
      {q.required && <span aria-hidden="true"> *</span>}
    </>
  );

  return (
    <Controller
      control={control}
      name={`answers.${q.id}`}
      render={({ field, fieldState }) => {
        const describedBy = [`${id}-hint`, fieldState.error ? `${id}-error` : undefined].filter(Boolean).join(" ");
        const input = (
          <Input
            question={q}
            id={id}
            value={field.value ?? ""}
            onChange={field.onChange}
            onBlur={field.onBlur}
            describedBy={describedBy}
            invalid={fieldState.invalid}
          />
        );
        const hint = (
          <FieldDescription id={`${id}-hint`}>
            {describeQuestion(q)}
            {!q.required && " · Optional"}
          </FieldDescription>
        );
        const error = fieldState.error && <FieldError id={`${id}-error`}>{fieldState.error.message}</FieldError>;

        // Several radios: a fieldset, named by its legend. One input: a label.
        return isGroupKind(q) ? (
          <fieldset className="flex flex-col gap-2" aria-describedby={describedBy} data-invalid={fieldState.invalid || undefined}>
            <legend className="mb-1 text-sm font-medium break-words">{label}</legend>
            {hint}
            {input}
            {error}
          </fieldset>
        ) : (
          <Field data-invalid={fieldState.invalid || undefined}>
            {/* block, not the label's default flex: the number, prompt and
                marker then flow as one line of text, as in the legend. */}
            <FieldLabel htmlFor={id} className="block break-words">
              {label}
            </FieldLabel>
            {hint}
            {input}
            {error}
          </Field>
        );
      }}
    />
  );
}

/** RestoredNotice says a draft was brought back, and anything about it the candidate should check. */
function RestoredNotice({ restored }: { restored: RestoredDraft }) {
  return (
    <div role="status" className="flex gap-3 rounded-lg border bg-surface p-4 text-sm">
      <InfoIcon className="mt-0.5 size-4 shrink-0 text-primary-ink" aria-hidden="true" />
      <div className="flex flex-col gap-1">
        <p className="font-medium">We restored the draft you saved on{" "}
          {new Date(restored.savedAt).toLocaleString("en-GB", { day: "numeric", month: "short", hour: "2-digit", minute: "2-digit" })}.</p>
        {restored.reworded.length > 0 && (
          <p className="text-text-secondary">
            {restored.reworded.length === 1 ? "One question was" : `${restored.reworded.length} questions were`} reworded since
            then; please reread your answer to: {restored.reworded.join("; ")}.
          </p>
        )}
        {restored.dropped.length > 0 && (
          <p className="text-text-secondary">
            {restored.dropped.length === 1 ? "One saved answer no longer fits" : `${restored.dropped.length} saved answers no longer fit`} the form
            and {restored.dropped.length === 1 ? "was" : "were"} left out: {restored.dropped.join("; ")}.
          </p>
        )}
      </div>
    </div>
  );
}

/**
 * DraftStatus saves the form half a second after the last change and says
 * so. The visible text updates every save; the screen-reader announcement
 * is made once, on the first save, and on a failure, so typing is not
 * interrupted by "draft saved" after every pause.
 */
function DraftStatus({
  control,
  userId,
  postId,
  questions,
}: {
  control: Control<ApplicationValues>;
  userId: string;
  postId: string;
  questions: Question[];
}) {
  // useWatch subscribes this component alone to every change, so the whole
  // form does not re-render on each keystroke just to save a draft.
  const values = useWatch({ control }) as ApplicationValues;
  const [state, setState] = useState<{ savedAt: Date | null; failed: boolean }>({ savedAt: null, failed: false });
  const [announcement, setAnnouncement] = useState("");
  const firstRun = useRef(true);
  const announced = useRef(false);

  useEffect(() => {
    // The values the form starts with are the draft itself (or nothing), so
    // there is nothing to save until something changes.
    if (firstRun.current) {
      firstRun.current = false;
      return;
    }
    const timer = setTimeout(() => {
      const ok = saveDraft(userId, postId, questions, values);
      setState({ savedAt: ok ? new Date() : null, failed: !ok });
      if (!ok) setAnnouncement("Your draft could not be saved in this browser.");
      else if (!announced.current) {
        announced.current = true;
        setAnnouncement("Your draft is being saved in this browser as you type.");
      }
    }, 500);
    // A new change within half a second cancels the pending save: one write
    // per pause in typing, not one per keystroke.
    return () => clearTimeout(timer);
  }, [values, userId, postId, questions]);

  return (
    <p className="flex items-center gap-1.5 text-sm text-muted-foreground">
      <span aria-live="polite" className="sr-only">
        {announcement}
      </span>
      {state.failed ? (
        <span className="text-destructive">Draft not saved: this browser is blocking storage.</span>
      ) : state.savedAt ? (
        <>
          <CheckIcon className="size-3.5" aria-hidden="true" />
          Draft saved at {state.savedAt.toLocaleTimeString("en-GB", { hour: "2-digit", minute: "2-digit" })}
        </>
      ) : (
        "Changes are saved as you type"
      )}
    </p>
  );
}

