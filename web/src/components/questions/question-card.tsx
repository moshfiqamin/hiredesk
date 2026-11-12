"use client";

import { useSortable } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { ArrowDownIcon, ArrowUpIcon, GripVerticalIcon, PlusIcon, Trash2Icon, XIcon } from "lucide-react";
import { Controller, useFieldArray, useFormContext, useWatch } from "react-hook-form";

import { Button } from "@/components/ui/button";
import { Field, FieldDescription, FieldError, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { NativeSelect } from "@/components/ui/native-select";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import type { QuestionKind } from "@/lib/api/types";
import { MAX_CHOICES, fieldsForKind, questionKinds, type QuestionFormValues } from "@/lib/questions/schema";

/**
 * QuestionCard edits one question. Its position changes with the move
 * buttons (keyboard, pointer, voice control), or by dragging the handle with
 * a mouse or finger.
 */
export function QuestionCard({
  sortableId,
  index,
  total,
  onMove,
  onRemove,
}: {
  sortableId: string;
  index: number;
  total: number;
  onMove: (from: number, to: number) => void;
  onRemove: (index: number) => void;
}) {
  const {
    register,
    setValue,
    control,
    formState: { errors },
  } = useFormContext<QuestionFormValues>();

  // useSortable makes this card one item of the sortable list: it supplies the
  // props for the drag handle and the transform to apply while dragging.
  const { listeners, setNodeRef, setActivatorNodeRef, transform, transition, isDragging } = useSortable({
    id: sortableId,
  });

  const kind = useWatch({ control, name: `questions.${index}.kind` });
  const prompt = useWatch({ control, name: `questions.${index}.prompt` });
  const fieldErrors = errors.questions?.[index];
  const label = prompt?.trim() ? `“${prompt.trim()}”` : `question ${index + 1}`;
  const id = (field: string) => `q-${sortableId}-${field}`;

  return (
    <li
      ref={setNodeRef}
      data-question={sortableId}
      style={{ transform: CSS.Transform.toString(transform), transition }}
      className={`rounded-lg border bg-surface ${isDragging ? "relative z-10 shadow-lg ring-2 ring-ring/40" : ""}`}
    >
      <div className="flex items-center gap-1 border-b px-2 py-1.5">
        {/* A pointer-only shortcut. Hidden from the keyboard and from
            assistive technology because the move buttons do the same job
            predictably; a second, flakier way to do it would only confuse. */}
        <span
          ref={setActivatorNodeRef}
          {...listeners}
          aria-hidden="true"
          title="Drag to reorder"
          className="flex size-7 cursor-grab touch-none items-center justify-center rounded text-muted-foreground hover:bg-surface-hover active:cursor-grabbing"
        >
          <GripVerticalIcon className="size-4" />
        </span>
        <span className="tabular text-sm font-medium text-muted-foreground">Question {index + 1}</span>
        <div className="ml-auto flex items-center gap-0.5">
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            data-move="up"
            aria-label={`Move ${label} up`}
            disabled={index === 0}
            onClick={() => onMove(index, index - 1)}
          >
            <ArrowUpIcon aria-hidden="true" />
          </Button>
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            data-move="down"
            aria-label={`Move ${label} down`}
            disabled={index === total - 1}
            onClick={() => onMove(index, index + 1)}
          >
            <ArrowDownIcon aria-hidden="true" />
          </Button>
          <Button type="button" variant="ghost" size="icon-sm" aria-label={`Remove ${label}`} onClick={() => onRemove(index)}>
            <Trash2Icon aria-hidden="true" />
          </Button>
        </div>
      </div>

      <div className="flex flex-col gap-4 p-4">
        <Field data-invalid={!!fieldErrors?.prompt}>
          <FieldLabel htmlFor={id("prompt")}>Question</FieldLabel>
          <Textarea
            id={id("prompt")}
            rows={2}
            aria-invalid={!!fieldErrors?.prompt}
            aria-describedby={fieldErrors?.prompt ? id("prompt-error") : undefined}
            {...register(`questions.${index}.prompt`)}
          />
          <FieldError id={id("prompt-error")} errors={[fieldErrors?.prompt]} />
        </Field>

        <div className="grid gap-4 sm:grid-cols-[1fr_auto]">
          <Field>
            <FieldLabel htmlFor={id("kind")}>Answer type</FieldLabel>
            <NativeSelect
              id={id("kind")}
              aria-describedby={id("kind-hint")}
              {...register(`questions.${index}.kind`, {
                // Switching kind resets the fields that belong to a kind, so a
                // minimum length or a list of options cannot outlive the kind
                // it was written for. shouldDirty marks the change as unsaved.
                onChange: (event) => {
                  const next = fieldsForKind(event.target.value as QuestionKind);
                  setValue(`questions.${index}.min_length`, next.min_length, { shouldDirty: true });
                  setValue(`questions.${index}.choices`, next.choices, { shouldDirty: true });
                },
              })}
            >
              {questionKinds.map((k) => (
                <option key={k.value} value={k.value}>
                  {k.label}
                </option>
              ))}
            </NativeSelect>
            <FieldDescription id={id("kind-hint")}>
              {questionKinds.find((k) => k.value === kind)?.hint}
            </FieldDescription>
          </Field>
          <Field orientation="horizontal" className="sm:mt-6 sm:w-auto">
            <Controller
              control={control}
              name={`questions.${index}.required`}
              render={({ field }) => (
                <Switch id={id("required")} checked={field.value} onCheckedChange={field.onChange} />
              )}
            />
            <FieldLabel htmlFor={id("required")} className="font-normal">
              Required
            </FieldLabel>
          </Field>
        </div>

        {/* Kind-specific fields: rendered only for the kind they belong to. */}
        {kind === "text" && (
          <Field data-invalid={!!fieldErrors?.min_length} className="sm:max-w-56">
            <FieldLabel htmlFor={id("min_length")}>Minimum length</FieldLabel>
            <div className="flex items-center gap-2">
              <Input
                id={id("min_length")}
                inputMode="numeric"
                className="tabular"
                aria-invalid={!!fieldErrors?.min_length}
                aria-describedby={id("min_length-hint")}
                {...register(`questions.${index}.min_length`)}
              />
              <span className="text-sm text-muted-foreground">characters</span>
            </div>
            <FieldDescription id={id("min_length-hint")}>0 means any length.</FieldDescription>
            <FieldError errors={[fieldErrors?.min_length]} />
          </Field>
        )}
        {kind === "choice" && <ChoiceEditor index={index} />}
      </div>
    </li>
  );
}

/** ChoiceEditor edits the options of a choice question. */
function ChoiceEditor({ index }: { index: number }) {
  const {
    register,
    control,
    formState: { errors },
  } = useFormContext<QuestionFormValues>();
  // A field array inside a field array: the options of this one question.
  const { fields, append, remove } = useFieldArray({ control, name: `questions.${index}.choices` });
  const choiceErrors = errors.questions?.[index]?.choices;
  // An issue about the list as a whole ("add at least one option") arrives
  // under .root; one about a single option under its index.
  const listError = choiceErrors?.root ?? (choiceErrors && "message" in choiceErrors ? choiceErrors : undefined);

  return (
    <fieldset className="flex flex-col gap-2">
      <legend className="mb-1.5 text-sm font-medium">Options</legend>
      {fields.length === 0 && (
        <p className="text-sm text-muted-foreground">Candidates pick one of the options you add here.</p>
      )}
      <ol className="flex flex-col gap-2">
        {fields.map((field, i) => {
          const error = choiceErrors?.[i]?.value;
          return (
            <li key={field.id} className="flex flex-col gap-1">
              <div className="flex items-center gap-2">
                <span className="tabular w-5 text-right text-xs text-muted-foreground" aria-hidden="true">
                  {i + 1}.
                </span>
                <Input
                  aria-label={`Option ${i + 1}`}
                  aria-invalid={!!error}
                  {...register(`questions.${index}.choices.${i}.value`)}
                />
                <Button type="button" variant="ghost" size="icon-sm" aria-label={`Remove option ${i + 1}`} onClick={() => remove(i)}>
                  <XIcon aria-hidden="true" />
                </Button>
              </div>
              {error && <FieldError className="ml-7">{error.message}</FieldError>}
            </li>
          );
        })}
      </ol>
      {listError?.message && <FieldError>{listError.message}</FieldError>}
      <div>
        <Button
          type="button"
          variant="outline"
          size="sm"
          disabled={fields.length >= MAX_CHOICES}
          // shouldFocus moves the cursor into the new, empty option.
          onClick={() => append({ value: "" }, { shouldFocus: true })}
        >
          <PlusIcon aria-hidden="true" />
          Add option
        </Button>
      </div>
    </fieldset>
  );
}
