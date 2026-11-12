"use client";

import {
  DndContext,
  PointerSensor,
  closestCenter,
  useSensor,
  useSensors,
  type Announcements,
  type DragEndEvent,
} from "@dnd-kit/core";
import { SortableContext, verticalListSortingStrategy } from "@dnd-kit/sortable";
import { zodResolver } from "@hookform/resolvers/zod";
import { useQueryClient } from "@tanstack/react-query";
import { AlertTriangleIcon, ListChecksIcon, LockIcon, PlusIcon } from "lucide-react";
import Link from "next/link";
import { useParams } from "next/navigation";
import { useRef, useState } from "react";
import { FormProvider, useFieldArray, useForm, useWatch } from "react-hook-form";
import { toast } from "sonner";

import { EmptyState, QueryView } from "@/components/states/states";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { useJobPost } from "@/hooks/use-job-posts";
import { useQuestions } from "@/hooks/use-questions";
import { useUnsavedChangesWarning } from "@/hooks/use-unsaved-changes";
import { api } from "@/lib/api/client";
import { ApiError } from "@/lib/api/errors";
import type { JobPost, Question } from "@/lib/api/types";
import { queryKeys } from "@/lib/query/keys";
import {
  blankQuestion,
  draftFrom,
  questionFormSchema,
  questionKinds,
  type QuestionDraft,
  type QuestionFormValues,
} from "@/lib/questions/schema";
import { QuestionSaveError, baselineFrom, saveQuestions } from "@/lib/questions/sync";

import { FormPreview } from "./form-preview";
import { QuestionCard } from "./question-card";

/** QuestionBuilder loads a role's form and shows the editor, or the locked view once the role has opened. */
export function QuestionBuilder() {
  const { id } = useParams<{ id: string }>();
  const post = useJobPost(id);
  const questions = useQuestions(id);

  return (
    <QueryView
      query={questions}
      errorTitle="Could not load the application form"
      loading={
        <div className="grid gap-6 lg:grid-cols-[1fr_22rem]" aria-busy="true" aria-label="Loading the form">
          <div className="flex flex-col gap-3">
            <Skeleton className="h-48" />
            <Skeleton className="h-48" />
          </div>
          <Skeleton className="h-80" />
        </div>
      }
    >
      {(data) =>
        !post.data ? null : post.data.status === "draft" ? (
          // Keyed by the post's updated_at: if the post changes underneath
          // (opened in another tab), the editor remounts from fresh data.
          <QuestionEditor key={post.data.updated_at} post={post.data} questions={data} />
        ) : (
          <LockedForm post={post.data} questions={data} />
        )
      }
    </QueryView>
  );
}

/**
 * LockedForm is the read-only view once a role has left draft. It says why
 * the form cannot be edited instead of only greying the controls out.
 */
function LockedForm({ post, questions }: { post: JobPost; questions: Question[] }) {
  const drafts = questions.map(draftFrom);
  return (
    <div className="flex flex-col gap-6">
      <div role="note" className="flex gap-3 rounded-lg border border-border-strong bg-surface p-4">
        <LockIcon className="mt-0.5 size-5 shrink-0 text-text-secondary" aria-hidden="true" />
        <div className="flex flex-col gap-1">
          <h2 className="font-medium">This form is locked because the role is {post.status}</h2>
          <p className="max-w-prose text-text-secondary">
            Candidates have been answering these exact questions since the role opened. Changing a question now would
            make earlier answers mean something different from later ones, and scores built on them would no longer
            compare fairly. To ask different questions, create a new role.
          </p>
          <div className="mt-1">
            <Link href="/admin/roles/new" className="text-sm font-medium text-primary-ink underline-offset-4 hover:underline">
              Create a new role
            </Link>
          </div>
        </div>
      </div>
      <div className="grid items-start gap-6 lg:grid-cols-[1fr_22rem]">
        <ol className="flex flex-col gap-3" aria-label="Questions">
          {drafts.map((q, i) => (
            <li key={q.questionId} className="rounded-lg border bg-surface p-4">
              <div className="flex flex-wrap items-baseline justify-between gap-2">
                <p className="font-medium">
                  <span className="tabular text-muted-foreground">{i + 1}. </span>
                  {q.prompt}
                </p>
                <span className="text-xs text-muted-foreground">
                  {questionKinds.find((k) => k.value === q.kind)?.label} · {q.required ? "Required" : "Optional"}
                  {q.kind === "text" && Number(q.min_length) > 0 && ` · at least ${q.min_length} characters`}
                </span>
              </div>
              {q.kind === "choice" && (
                <p className="mt-1 text-sm text-text-secondary">{q.choices.map((c) => c.value).join(" · ")}</p>
              )}
            </li>
          ))}
        </ol>
        <FormPreview questions={drafts} title={post.title} />
      </div>
    </div>
  );
}

function QuestionEditor({ post, questions }: { post: JobPost; questions: Question[] }) {
  const queryClient = useQueryClient();
  const form = useForm<QuestionFormValues>({
    resolver: zodResolver(questionFormSchema),
    defaultValues: { questions: questions.map(draftFrom) },
    mode: "onTouched",
  });
  const {
    control,
    handleSubmit,
    setValue,
    setError,
    reset,
    getValues,
    formState: { isDirty, isSubmitting, errors },
  } = form;
  // useFieldArray gives each row a stable `id` (used here as the sortable ID
  // and React key) and add, remove and move operations that keep values,
  // errors and dirty state attached to the right row.
  const { fields, append, remove, move } = useFieldArray({ control, name: "questions" });
  const drafts = useWatch({ control, name: "questions" }) as QuestionDraft[];

  // What the server holds, updated after every request that succeeds, so a
  // retry after a partial failure picks up where it stopped.
  const baseline = useRef(baselineFrom(questions));
  const addButton = useRef<HTMLButtonElement>(null);
  const [announcement, setAnnouncement] = useState("");

  useUnsavedChangesWarning(isDirty && !isSubmitting);

  // Pointer only: mouse and touch drag after 4px of movement, so a click on
  // the handle is still a click. There is deliberately no KeyboardSensor; the
  // move buttons are the keyboard path (see docs/notes/11-5).
  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 4 } }));

  const describe = (key: string | number) => {
    const index = fields.findIndex((f) => f.id === key);
    const prompt = getValues(`questions.${index}.prompt`)?.trim();
    return prompt ? `“${prompt}”` : `question ${index + 1}`;
  };
  const position = (key: string | number) => `position ${fields.findIndex((f) => f.id === key) + 1} of ${fields.length}`;

  // What dnd-kit's own live region says at each step of a pointer drag, so a
  // screen reader user dragging with a mouse hears where the question is.
  const announcements: Announcements = {
    onDragStart: ({ active }) => `Picked up ${describe(active.id)}, at ${position(active.id)}.`,
    onDragOver: ({ active, over }) => (over ? `${describe(active.id)} is over ${position(over.id)}.` : undefined),
    onDragEnd: ({ active, over }) =>
      over ? `Dropped ${describe(active.id)} at ${position(over.id)}.` : `${describe(active.id)} was dropped.`,
    onDragCancel: ({ active }) => `Reordering cancelled. ${describe(active.id)} is back where it was.`,
  };

  function onDragEnd({ active, over }: DragEndEvent) {
    if (!over || active.id === over.id) return;
    move(
      fields.findIndex((f) => f.id === active.id),
      fields.findIndex((f) => f.id === over.id),
    );
  }

  function moveBy(from: number, to: number) {
    const key = fields[from].id;
    const label = describe(key);
    move(from, to);
    // The move buttons have no drag announcements, so they get their own.
    setAnnouncement(`Moved ${label} to position ${to + 1} of ${fields.length}.`);
    // Focus stays on the button that was pressed, which moved with its card.
    // At the top or bottom that button becomes disabled and would lose focus,
    // so focus moves to the card's other move button instead.
    const edge = to === 0 ? "down" : to === fields.length - 1 ? "up" : null;
    if (edge) {
      requestAnimationFrame(() =>
        document.querySelector<HTMLButtonElement>(`[data-question="${key}"] [data-move="${edge}"]`)?.focus(),
      );
    }
  }

  function removeAt(index: number) {
    const label = describe(fields[index].id);
    remove(index);
    setAnnouncement(`Removed ${label}. Save to apply.`);
    // The focused button has just disappeared; move focus somewhere stable
    // rather than letting it fall back to the top of the page.
    addButton.current?.focus();
  }

  async function onSubmit(values: QuestionFormValues) {
    try {
      await saveQuestions(post.id, values.questions, baseline.current, (index, created) =>
        // Records the new ID in the builder, so a retry patches this question
        // instead of creating it a second time.
        setValue(`questions.${index}.questionId`, created.id),
      );
      const fresh = await queryClient.fetchQuery({
        queryKey: queryKeys.jobPosts.questions(post.id),
        queryFn: () => api<{ data: Question[] }>(`/v1/job-posts/${post.id}/questions`).then((b) => b.data),
        // Always ask the server: the point is to read back what was saved.
        staleTime: 0,
      });
      baseline.current = baselineFrom(fresh);
      reset({ questions: fresh.map(draftFrom) });
      void queryClient.invalidateQueries({ queryKey: queryKeys.jobPosts.detail(post.id) });
      toast.success("Application form saved");
    } catch (error) {
      const cause = error instanceof QuestionSaveError ? error.cause : error;
      if (cause instanceof ApiError && cause.code === "form_locked") {
        toast.error("The form was locked", { description: "The role was opened while you were editing." });
        void queryClient.invalidateQueries({ queryKey: queryKeys.jobPosts.detail(post.id) });
        return;
      }
      if (error instanceof QuestionSaveError && error.index !== null && cause instanceof ApiError && cause.code === "validation_failed") {
        // Map the 422 onto that question's own fields.
        for (const [field, problem] of Object.entries(cause.details)) {
          const path = field === "choices" ? `questions.${error.index}.choices.root` : `questions.${error.index}.${field}`;
          setError(path as `questions.${number}.prompt`, { type: "server", message: String(problem) });
        }
        return;
      }
      setError("root.server", { type: "server", message: error instanceof Error ? error.message : "Could not save." });
    }
  }

  return (
    // FormProvider shares this form with every QuestionCard through context,
    // instead of passing register, control and errors down by hand.
    <FormProvider {...form}>
      <form
        // handleSubmit is called inside the event handler, not during render:
        // onSubmit reads the baseline ref, and refs must not be read while
        // rendering (the value is not part of React's state, so a render
        // would not re-run when it changes).
        onSubmit={(event) => void handleSubmit(onSubmit)(event)}
        noValidate
        className="grid items-start gap-6 lg:grid-cols-[1fr_22rem]"
      >
        <div className="flex flex-col gap-4">
          {errors.root?.server && (
            <div role="alert" className="flex items-start gap-2 rounded-lg border border-destructive/40 bg-surface p-3 text-sm">
              <AlertTriangleIcon className="mt-0.5 size-4 shrink-0 text-destructive" aria-hidden="true" />
              <p>{errors.root.server.message} Saved changes so far are kept; press Save again to finish.</p>
            </div>
          )}

          {fields.length === 0 ? (
            <EmptyState icon={ListChecksIcon} title="No questions yet">
              Candidates answer these when they apply, and the rubric scores the answers. A role needs at least one
              question before it can open.
            </EmptyState>
          ) : (
            <DndContext
              sensors={sensors}
              collisionDetection={closestCenter}
              onDragEnd={onDragEnd}
              accessibility={{ announcements }}
            >
              <SortableContext items={fields.map((f) => f.id)} strategy={verticalListSortingStrategy}>
                <ol className="flex flex-col gap-3" aria-label="Questions">
                  {fields.map((field, index) => (
                    <QuestionCard
                      key={field.id}
                      sortableId={field.id}
                      index={index}
                      total={fields.length}
                      onMove={moveBy}
                      onRemove={removeAt}
                    />
                  ))}
                </ol>
              </SortableContext>
            </DndContext>
          )}

          <p className="sr-only" role="status" aria-live="polite">
            {announcement}
          </p>

          <div>
            <Button
              ref={addButton}
              type="button"
              variant="outline"
              onClick={() => append(blankQuestion(), { shouldFocus: true })}
            >
              <PlusIcon aria-hidden="true" />
              Add question
            </Button>
          </div>

          <div className="sticky bottom-0 -mx-1 flex flex-wrap items-center gap-3 border-t bg-background/95 px-1 py-3 backdrop-blur">
            <Button type="submit" size="lg" disabled={!isDirty || isSubmitting}>
              {isSubmitting ? "Saving…" : "Save form"}
            </Button>
            <Button type="button" variant="ghost" size="lg" disabled={!isDirty || isSubmitting} onClick={() => reset()}>
              Discard changes
            </Button>
            <span className="text-sm text-muted-foreground" aria-live="polite">
              {isDirty ? "Unsaved changes" : "All changes saved"}
            </span>
          </div>
        </div>

        <div className="lg:sticky lg:top-6">
          <FormPreview questions={drafts ?? []} title={post.title} />
        </div>
      </form>
    </FormProvider>
  );
}
