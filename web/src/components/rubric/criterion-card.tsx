"use client";

import { LightbulbIcon, Trash2Icon } from "lucide-react";
import { Controller, useFormContext, useWatch } from "react-hook-form";

import { Button } from "@/components/ui/button";
import { FieldError } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { NativeSelect } from "@/components/ui/native-select";
import type { CriterionKind, Question } from "@/lib/api/types";
import { NoQuestionsHint, criterionKinds, kindOf, questionKindLabel, type CriterionConfig } from "@/lib/rubric/kinds";
import { toHundredths, type RubricFormValues } from "@/lib/rubric/schema";

/** CriterionCard edits one criterion: its name, kind, weight, the question it reads, and its config. */
export function CriterionCard({
  index,
  number,
  questions,
  roleId,
  onRemove,
  toneClass,
}: {
  index: number;
  number: number;
  questions: Question[];
  roleId: string;
  onRemove: () => void;
  toneClass: string;
}) {
  const {
    register,
    control,
    setValue,
    getValues,
    clearErrors,
    formState: { errors },
  } = useFormContext<RubricFormValues>();
  const kind = useWatch({ control, name: `criteria.${index}.kind` });
  const weight = useWatch({ control, name: `criteria.${index}.weight` });
  const config = useWatch({ control, name: `criteria.${index}.config` }) as CriterionConfig;
  const label = useWatch({ control, name: `criteria.${index}.label` });

  // Everything kind-specific comes from the registry: no branching on kind here.
  const def = kindOf(kind);
  const readable = questions.filter((q) => def.questionKinds.includes(q.kind));
  const fieldErrors = errors.criteria?.[index];
  const hundredths = toHundredths(weight ?? "");
  const example = hundredths !== null && hundredths > 0 ? def.example(config, hundredths / 100) : null;
  const id = (f: string) => `c${index}-${f}`;
  const name = label?.trim() ? `“${label.trim()}”` : `criterion ${number}`;

  function changeKind(next: CriterionKind) {
    const nextDef = kindOf(next);
    // A new kind gets that kind's empty config: the old one has a different
    // shape and cannot be carried over. The chosen question survives only if
    // the new kind can read it.
    setValue(`criteria.${index}.config`, nextDef.empty(), { shouldDirty: true });
    const current = questions.find((q) => q.id === getValues(`criteria.${index}.question_id`));
    if (!nextDef.needsQuestion || !current || !nextDef.questionKinds.includes(current.kind)) {
      setValue(`criteria.${index}.question_id`, "", { shouldDirty: true });
    }
    // Messages written for the old kind ("choose the written or choice
    // question") no longer describe this one. setValue does not re-validate,
    // so they are cleared here and re-checked on the next edit or on save.
    clearErrors([`criteria.${index}.question_id`, `criteria.${index}.config`]);
  }

  return (
    <li className="rounded-lg border bg-surface">
      <div className="flex items-center gap-2 border-b px-3 py-2">
        <span aria-hidden="true" className={`flex size-6 items-center justify-center rounded font-mono text-xs ${toneClass}`}>
          {number}
        </span>
        <h3 className="min-w-0 flex-1 truncate text-sm font-medium">{label?.trim() || `Criterion ${number}`}</h3>
        <Button type="button" variant="ghost" size="icon-sm" aria-label={`Remove ${name}`} onClick={onRemove}>
          <Trash2Icon aria-hidden="true" />
        </Button>
      </div>

      <div className="flex flex-col gap-5 p-4">
        <div className="grid gap-4 sm:grid-cols-[1fr_9rem]">
          <div className="flex flex-col gap-1.5">
            <label htmlFor={id("label")} className="text-sm font-medium">
              Name
            </label>
            <Input
              id={id("label")}
              placeholder="e.g. Go experience"
              aria-invalid={!!fieldErrors?.label}
              {...register(`criteria.${index}.label`)}
            />
            <FieldError errors={[fieldErrors?.label]} />
          </div>
          <div className="flex flex-col gap-1.5">
            <label htmlFor={id("weight")} className="text-sm font-medium">
              Weight
            </label>
            <div className="flex items-center gap-2">
              <Input
                id={id("weight")}
                inputMode="decimal"
                className="text-right font-mono tabular-nums"
                aria-invalid={!!fieldErrors?.weight}
                aria-describedby={id("weight-hint")}
                {...register(`criteria.${index}.weight`)}
              />
              <span className="text-sm text-muted-foreground">pts</span>
            </div>
            <p id={id("weight-hint")} className="sr-only">
              The most points this criterion can award. All weights together must make 100.
            </p>
            <FieldError errors={[fieldErrors?.weight]} />
          </div>
        </div>

        {/* The kinds as a radio group with their summaries in view: what each
            one does is visible while choosing, not behind a tooltip. */}
        <fieldset>
          <legend className="mb-2 text-sm font-medium">How it scores</legend>
          <div className="grid gap-2 sm:grid-cols-2">
            {(Object.keys(criterionKinds) as CriterionKind[]).map((k) => {
              const option = kindOf(k);
              return (
                <label
                  key={k}
                  className="flex cursor-pointer gap-2.5 rounded-md border p-2.5 text-sm transition-colors hover:bg-surface-hover has-checked:border-primary has-checked:bg-surface-selected has-focus-visible:ring-3 has-focus-visible:ring-ring/50"
                >
                  <input
                    type="radio"
                    value={k}
                    className="mt-0.5 accent-(--primary)"
                    {...register(`criteria.${index}.kind`, { onChange: (e) => changeKind(e.target.value as CriterionKind) })}
                  />
                  <span>
                    <span className="block font-medium">{option.label}</span>
                    <span className="block text-xs text-text-secondary">{option.summary}</span>
                  </span>
                </label>
              );
            })}
          </div>
        </fieldset>

        {def.needsQuestion && (
          <div className="flex flex-col gap-1.5">
            <label htmlFor={id("question")} className="text-sm font-medium">
              Question it reads
            </label>
            {readable.length === 0 ? (
              <NoQuestionsHint roleId={roleId} kinds={def.questionKinds.map((k) => questionKindLabel[k]).join(" or ")} />
            ) : (
              <NativeSelect
                id={id("question")}
                aria-invalid={!!fieldErrors?.question_id}
                {...register(`criteria.${index}.question_id`)}
              >
                <option value="">Choose a question…</option>
                {/* Only this role's questions, and only the kinds this
                    criterion can score. */}
                {readable.map((q) => (
                  <option key={q.id} value={q.id}>
                    {q.position}. {q.prompt}
                  </option>
                ))}
              </NativeSelect>
            )}
            <FieldError errors={[fieldErrors?.question_id]} />
          </div>
        )}

        <Controller
          control={control}
          name={`criteria.${index}.config`}
          render={({ field }) => (
            <def.Editor
              value={field.value as CriterionConfig}
              onChange={field.onChange}
              questions={readable}
              idPrefix={id("config")}
              errors={fieldErrors?.config as Record<string, { message?: string }> | undefined}
              roleId={roleId}
            />
          )}
        />
        {/* A problem the server reports about the config as a whole. */}
        {fieldErrors?.config?.message && <FieldError>{fieldErrors.config.message}</FieldError>}

        {example && (
          <p className="flex gap-2 rounded-md bg-surface-sunken px-3 py-2 text-sm text-text-secondary">
            <LightbulbIcon className="mt-0.5 size-4 shrink-0" aria-hidden="true" />
            <span>
              <span className="font-medium text-foreground">For example: </span>
              {example}
            </span>
          </p>
        )}
      </div>
    </li>
  );
}
