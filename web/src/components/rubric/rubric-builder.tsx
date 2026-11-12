"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { AlertTriangleIcon, PlusIcon, ScaleIcon } from "lucide-react";
import Link from "next/link";
import { useParams } from "next/navigation";
import { useRef } from "react";
import { FormProvider, useFieldArray, useForm, useWatch } from "react-hook-form";
import { toast } from "sonner";

import { EmptyState, ErrorState } from "@/components/states/states";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { useJobPost } from "@/hooks/use-job-posts";
import { useQuestions } from "@/hooks/use-questions";
import { useReplaceRubric, useRescoreAll, useRubric } from "@/hooks/use-rubric";
import { useUnsavedChangesWarning } from "@/hooks/use-unsaved-changes";
import { ApiError } from "@/lib/api/errors";
import type { JobPost, Question, Rubric } from "@/lib/api/types";
import { blankCriterion, draftFrom, rubricFormSchema, toInput, type RubricFormValues } from "@/lib/rubric/schema";

import { CriterionCard } from "./criterion-card";
import { WeightPanel, tones, weightTotal } from "./weight-panel";

/** RubricBuilder loads what the builder depends on: the role, its questions and its current rubric. */
export function RubricBuilder() {
  const { id } = useParams<{ id: string }>();
  const post = useJobPost(id);
  const questions = useQuestions(id);
  const rubric = useRubric(id);

  const failed = [questions, rubric].find((q) => q.isError && !q.data);
  if (failed) {
    return <ErrorState error={failed.error} title="Could not load the rubric" onRetry={() => void failed.refetch()} />;
  }
  if (!post.data || !questions.data || !rubric.data) {
    return (
      <div className="grid gap-6 lg:grid-cols-[1fr_20rem]" aria-busy="true" aria-label="Loading the rubric">
        <div className="flex flex-col gap-3">
          <Skeleton className="h-72" />
          <Skeleton className="h-72" />
        </div>
        <Skeleton className="h-80" />
      </div>
    );
  }

  return (
    // Keyed by version, so after a save elsewhere the form restarts from the
    // new version rather than holding the old one.
    <RubricEditor key={rubric.data.version ?? "none"} post={post.data} questions={questions.data} rubric={rubric.data} />
  );
}

function RubricEditor({ post, questions, rubric }: { post: JobPost; questions: Question[]; rubric: Rubric }) {
  const replace = useReplaceRubric(post.id);
  const rescore = useRescoreAll(post.id);
  const addButton = useRef<HTMLButtonElement>(null);

  const form = useForm<RubricFormValues>({
    resolver: zodResolver(rubricFormSchema(questions)),
    defaultValues: { criteria: rubric.criteria.map(draftFrom) },
    mode: "onTouched",
  });
  const {
    control,
    handleSubmit,
    reset,
    setError,
    formState: { isDirty, isSubmitting, errors },
  } = form;
  const { fields, append, remove } = useFieldArray({ control, name: "criteria" });

  // The total is derived: computed from the criteria on every render, never
  // stored in a state of its own that could disagree with them.
  const criteria = useWatch({ control, name: "criteria" }) as RubricFormValues["criteria"];
  const { state } = weightTotal(criteria ?? []);

  useUnsavedChangesWarning(isDirty && !isSubmitting);

  async function onSubmit(values: RubricFormValues) {
    try {
      const saved = await replace.mutateAsync(values.criteria.map(toInput));
      reset({ criteria: saved.criteria.map(draftFrom) });
      const stale = saved.applications_to_rescore;
      toast.success(`Rubric saved as version ${saved.version}`, {
        description:
          stale > 0
            ? `${stale} ${stale === 1 ? "application still has a score" : "applications still have scores"} from an older version.`
            : undefined,
        // Offered, not done automatically: rescoring rewrites every score
        // and appends an event to every application's timeline.
        action:
          stale > 0
            ? {
                label: "Rescore all",
                onClick: () =>
                  rescore.mutate(undefined, {
                    onSuccess: (r) => toast.success(`Rescored ${r.rescored} applications; ${r.changed} changed`),
                    onError: (e) => toast.error("Could not rescore", { description: e.message }),
                  }),
              }
            : undefined,
      });
    } catch (error) {
      if (error instanceof ApiError && error.code === "validation_failed") {
        // Server keys look like "criteria[2].config.question_ids[0]"; the
        // form's paths are "criteria.2.config.question_ids".
        const unmatched: string[] = [];
        for (const [key, problem] of Object.entries(error.details)) {
          const match = /^criteria\[(\d+)\]\.([a-z_]+)(?:\.([a-z_]+))?/.exec(key);
          if (match) {
            const path = [`criteria.${match[1]}.${match[2]}`, match[3]].filter(Boolean).join(".");
            setError(path as `criteria.${number}.label`, { type: "server", message: String(problem) });
          } else {
            unmatched.push(`${key} ${problem}`);
          }
        }
        if (unmatched.length) setError("root.server", { type: "server", message: unmatched.join("; ") });
        return;
      }
      setError("root.server", { type: "server", message: error instanceof Error ? error.message : "Could not save." });
    }
  }

  const canSave = isDirty && state === "exact" && !isSubmitting;
  // Why Save is unavailable, in words, beside the button.
  const reason = isSubmitting
    ? null
    : state !== "exact"
      ? "Save is available once the weights add up to exactly 100."
      : !isDirty
        ? rubric.version === null
          ? "Add criteria to build the rubric."
          : "No changes since the last save."
        : null;

  return (
    <FormProvider {...form}>
      <form
        onSubmit={(event) => void handleSubmit(onSubmit)(event)}
        noValidate
        className="grid items-start gap-6 lg:grid-cols-[1fr_20rem]"
      >
        <div className="flex flex-col gap-4">
          <p className="max-w-prose text-text-secondary">
            The rubric turns each application&apos;s answers into a score out of 100. Every criterion reads one or more
            answers and awards up to its weight in points.{" "}
            {rubric.version !== null && (
              <>
                This is version {rubric.version}; saving creates version {rubric.version + 1}, and existing scores keep
                the version that produced them until you rescore.
              </>
            )}
          </p>

          {questions.length === 0 && (
            <div className="rounded-lg border border-border-strong bg-surface p-3 text-sm">
              Criteria score answers, so this role needs questions first.{" "}
              <Link href={`/admin/roles/${post.id}/questions`} className="font-medium text-primary-ink underline-offset-4 hover:underline">
                Add questions
              </Link>
            </div>
          )}

          {errors.root?.server && (
            <div role="alert" className="flex items-start gap-2 rounded-lg border border-destructive/40 bg-surface p-3 text-sm">
              <AlertTriangleIcon className="mt-0.5 size-4 shrink-0 text-destructive" aria-hidden="true" />
              <p>{errors.root.server.message}</p>
            </div>
          )}

          {fields.length === 0 ? (
            <EmptyState icon={ScaleIcon} title="No criteria yet">
              Add a criterion for each thing that makes a strong application: the right keywords in an answer, enough
              years of experience, a thorough written reply, or a set of yes answers.
            </EmptyState>
          ) : (
            <ol className="flex flex-col gap-3" aria-label="Criteria">
              {fields.map((field, index) => (
                <CriterionCard
                  key={field.id}
                  index={index}
                  number={index + 1}
                  questions={questions}
                  roleId={post.id}
                  toneClass={tones[index % tones.length]}
                  onRemove={() => {
                    remove(index);
                    addButton.current?.focus();
                  }}
                />
              ))}
            </ol>
          )}

          <div>
            <Button ref={addButton} type="button" variant="outline" onClick={() => append(blankCriterion(), { shouldFocus: true })}>
              <PlusIcon aria-hidden="true" />
              Add criterion
            </Button>
          </div>
        </div>

        <div className="lg:sticky lg:top-6">
          <WeightPanel
            criteria={criteria ?? []}
            action={
              <div className="flex flex-col gap-2">
                <div className="flex gap-2">
                  <Button type="submit" size="lg" className="flex-1" disabled={!canSave}>
                    {isSubmitting ? "Saving…" : "Save rubric"}
                  </Button>
                  <Button type="button" variant="ghost" size="lg" disabled={!isDirty || isSubmitting} onClick={() => reset()}>
                    Discard
                  </Button>
                </div>
                {reason && <p className="text-xs text-muted-foreground">{reason}</p>}
              </div>
            }
          />
        </div>
      </form>
    </FormProvider>
  );
}
