"use client";

import Link from "next/link";
import type { ComponentType } from "react";

import { KeywordInput } from "@/components/rubric/keyword-input";
import { Checkbox } from "@/components/ui/checkbox";
import { FieldError } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import type { CriterionKind, Question, QuestionKind } from "@/lib/api/types";
import { formatNumber } from "@/lib/format";

// Config as the form holds it. Numbers stay text until they are sent, as in
// every other form here, so a half-typed "1." is not rejected mid-keystroke.
export interface KeywordConfig {
  keywords: string[];
}
export interface ExperienceConfig {
  min: string;
  target: string;
}
export interface DepthConfig {
  min_chars: string;
  target_chars: string;
}
export interface BooleanConfig {
  question_ids: string[];
}
export type CriterionConfig = KeywordConfig | ExperienceConfig | DepthConfig | BooleanConfig;

type ConfigErrors = Record<string, { message?: string } | undefined> | undefined;

export interface EditorProps<C> {
  value: C;
  onChange: (config: C) => void;
  /** Questions of this role that this kind can read. */
  questions: Question[];
  idPrefix: string;
  errors: ConfigErrors;
  roleId: string;
}

/**
 * KindDefinition is everything the builder needs to know about one criterion
 * kind. The builder never asks "which kind is this?"; it looks the kind up
 * here and uses what it finds, so adding a fifth kind is one new entry, not
 * a new branch in every component.
 */
export interface KindDefinition<C extends CriterionConfig> {
  label: string;
  /** What the kind does, in one sentence an admin can act on. */
  summary: string;
  /** Whether it reads one question named by question_id. */
  needsQuestion: boolean;
  /** The question kinds it can score, as scoring/engine.go's registry says. */
  questionKinds: QuestionKind[];
  empty: () => C;
  fromApi: (config: Record<string, unknown>) => C;
  toApi: (config: C) => Record<string, unknown>;
  /** Reports each problem with the config, at a path inside it. */
  check: (config: C, report: (path: string, message: string) => void) => void;
  /** A worked example with the current numbers, or null if they are not usable yet. */
  example: (config: C, weight: number) => string | null;
  Editor: ComponentType<EditorProps<C>>;
}

const isWhole = (s: string) => /^\d+$/.test(s.trim());
const isAmount = (s: string) => /^\d+(\.\d+)?$/.test(s.trim());
const points = (n: number) => `${formatNumber(Math.round(n * 100) / 100)} ${n === 1 ? "point" : "points"}`;
const years = (n: number) => `${formatNumber(n)} ${n === 1 ? "year" : "years"}`;

/** A pair of number inputs, the shape both ramp kinds use. */
function RangeEditor({
  idPrefix,
  errors,
  fields,
  unit,
}: {
  idPrefix: string;
  errors: ConfigErrors;
  unit: string;
  fields: { key: string; label: string; value: string; onChange: (v: string) => void; hint: string }[];
}) {
  return (
    <div className="grid gap-4 sm:grid-cols-2">
      {fields.map((f) => (
        <div key={f.key} className="flex flex-col gap-1.5">
          <label htmlFor={`${idPrefix}-${f.key}`} className="text-sm font-medium">
            {f.label}
          </label>
          <div className="flex items-center gap-2">
            <Input
              id={`${idPrefix}-${f.key}`}
              inputMode="decimal"
              className="tabular"
              value={f.value}
              aria-invalid={!!errors?.[f.key]}
              aria-describedby={`${idPrefix}-${f.key}-hint`}
              onChange={(e) => f.onChange(e.target.value)}
            />
            <span className="text-sm whitespace-nowrap text-muted-foreground">{unit}</span>
          </div>
          <p id={`${idPrefix}-${f.key}-hint`} className="text-xs text-muted-foreground">
            {f.hint}
          </p>
          <FieldError errors={[errors?.[f.key]]} />
        </div>
      ))}
    </div>
  );
}

const keywordMatch: KindDefinition<KeywordConfig> = {
  label: "Keyword match",
  summary: "Scores the share of your keywords found in a written answer, as whole words, ignoring case.",
  needsQuestion: true,
  questionKinds: ["text", "choice"],
  empty: () => ({ keywords: [] }),
  fromApi: (c) => ({ keywords: Array.isArray(c.keywords) ? c.keywords.map(String) : [] }),
  toApi: (c) => ({ keywords: c.keywords }),
  check: (c, report) => {
    if (c.keywords.length === 0) report("keywords", "Add at least one keyword");
  },
  example: (c, weight) => {
    const n = c.keywords.length;
    if (n < 2) return n === 1 ? `Using the keyword earns all ${points(weight)}; leaving it out earns none.` : null;
    const half = Math.floor(n / 2);
    return `An answer using ${half} of these ${n} keywords earns ${points((weight * half) / n)} of ${points(weight)}.`;
  },
  Editor: ({ value, onChange, idPrefix, errors }) => (
    <div className="flex flex-col gap-1.5">
      <label htmlFor={`${idPrefix}-keywords`} className="text-sm font-medium">
        Keywords
      </label>
      <KeywordInput
        id={`${idPrefix}-keywords`}
        value={value.keywords}
        onChange={(keywords) => onChange({ keywords })}
        invalid={!!errors?.keywords}
        describedBy={`${idPrefix}-keywords-hint`}
      />
      <p id={`${idPrefix}-keywords-hint`} className="text-xs text-muted-foreground">
        “go” matches “Go” and “go,” but not “Google”.
      </p>
      <FieldError errors={[errors?.keywords]} />
    </div>
  ),
};

const experienceYears: KindDefinition<ExperienceConfig> = {
  label: "Experience in years",
  summary: "Reads a number of years: nothing at or below the minimum, full weight at the target, a straight line between.",
  needsQuestion: true,
  questionKinds: ["number"],
  empty: () => ({ min: "0", target: "" }),
  fromApi: (c) => ({ min: String(c.min ?? 0), target: c.target === undefined ? "" : String(c.target) }),
  toApi: (c) => ({ min: Number(c.min), target: Number(c.target) }),
  check: (c, report) => {
    if (!isAmount(c.min)) report("min", "Enter a number of years, 0 or more");
    if (!isAmount(c.target)) report("target", "Enter the number of years that earns full marks");
    else if (isAmount(c.min) && Number(c.target) <= Number(c.min)) report("target", "Must be more than the minimum");
  },
  example: (c, weight) => {
    if (!isAmount(c.min) || !isAmount(c.target) || Number(c.target) <= Number(c.min)) return null;
    const min = Number(c.min);
    const target = Number(c.target);
    const mid = (min + target) / 2;
    return `${years(min)} or fewer earns nothing; ${formatNumber(target)} or more earns all ${points(weight)}; ${years(mid)} earns ${points(weight / 2)}.`;
  },
  Editor: ({ value, onChange, idPrefix, errors }) => (
    <RangeEditor
      idPrefix={idPrefix}
      errors={errors}
      unit="years"
      fields={[
        { key: "min", label: "Minimum", value: value.min, onChange: (min) => onChange({ ...value, min }), hint: "At or below this, no points." },
        { key: "target", label: "Target", value: value.target, onChange: (target) => onChange({ ...value, target }), hint: "At or above this, full points." },
      ]}
    />
  ),
};

const answerDepth: KindDefinition<DepthConfig> = {
  label: "Answer depth",
  summary: "Rewards fuller written answers: nothing at the minimum length, full weight at the target length, a straight line between.",
  needsQuestion: true,
  questionKinds: ["text"],
  empty: () => ({ min_chars: "0", target_chars: "" }),
  fromApi: (c) => ({
    min_chars: String(c.min_chars ?? 0),
    target_chars: c.target_chars === undefined ? "" : String(c.target_chars),
  }),
  toApi: (c) => ({ min_chars: Number(c.min_chars), target_chars: Number(c.target_chars) }),
  check: (c, report) => {
    if (!isWhole(c.min_chars)) report("min_chars", "Enter a whole number of characters, 0 or more");
    if (!isWhole(c.target_chars)) report("target_chars", "Enter the length that earns full marks");
    else if (isWhole(c.min_chars) && Number(c.target_chars) <= Number(c.min_chars))
      report("target_chars", "Must be more than the minimum");
  },
  example: (c, weight) => {
    if (!isWhole(c.min_chars) || !isWhole(c.target_chars) || Number(c.target_chars) <= Number(c.min_chars)) return null;
    const min = Number(c.min_chars);
    const target = Number(c.target_chars);
    const mid = Math.round((min + target) / 2);
    return `A ${formatNumber(mid)}-character answer earns about ${points(weight / 2)} of ${points(weight)}; ${formatNumber(target)} characters or more earns all of it.`;
  },
  Editor: ({ value, onChange, idPrefix, errors }) => (
    <RangeEditor
      idPrefix={idPrefix}
      errors={errors}
      unit="characters"
      fields={[
        {
          key: "min_chars",
          label: "Minimum length",
          value: value.min_chars,
          onChange: (min_chars) => onChange({ ...value, min_chars }),
          hint: "At or below this, no points.",
        },
        {
          key: "target_chars",
          label: "Target length",
          value: value.target_chars,
          onChange: (target_chars) => onChange({ ...value, target_chars }),
          hint: "At or above this, full points.",
        },
      ]}
    />
  ),
};

const booleanAll: KindDefinition<BooleanConfig> = {
  label: "All yes",
  summary: "Full weight only if the candidate answered Yes to every question you tick; any No earns nothing.",
  needsQuestion: false,
  questionKinds: ["boolean"],
  empty: () => ({ question_ids: [] }),
  fromApi: (c) => ({ question_ids: Array.isArray(c.question_ids) ? c.question_ids.map(String) : [] }),
  toApi: (c) => ({ question_ids: c.question_ids }),
  check: (c, report) => {
    if (c.question_ids.length === 0) report("question_ids", "Tick at least one yes/no question");
  },
  example: (c, weight) => {
    const n = c.question_ids.length;
    if (n === 0) return null;
    return n === 1
      ? `Yes earns ${points(weight)}; No earns nothing.`
      : `Yes to all ${n} earns ${points(weight)}; a single No earns nothing.`;
  },
  Editor: ({ value, onChange, questions, idPrefix, errors, roleId }) => (
    <fieldset className="flex flex-col gap-2">
      <legend className="mb-1.5 text-sm font-medium">Yes/no questions that must all be Yes</legend>
      {questions.length === 0 ? (
        <NoQuestionsHint roleId={roleId} kinds="yes/no" />
      ) : (
        questions.map((q) => {
          const checked = value.question_ids.includes(q.id);
          const id = `${idPrefix}-q-${q.id}`;
          return (
            <div key={q.id} className="flex items-start gap-2">
              <Checkbox
                id={id}
                checked={checked}
                onCheckedChange={(on) =>
                  onChange({
                    question_ids: on ? [...value.question_ids, q.id] : value.question_ids.filter((x) => x !== q.id),
                  })
                }
                className="mt-0.5"
              />
              <label htmlFor={id} className="text-sm">
                {q.prompt}
              </label>
            </div>
          );
        })
      )}
      <FieldError errors={[errors?.question_ids]} />
    </fieldset>
  ),
};

/** NoQuestionsHint explains an empty question list and links to where to fix it. */
export function NoQuestionsHint({ roleId, kinds }: { roleId: string; kinds: string }) {
  return (
    <p className="rounded-md bg-surface-sunken px-3 py-2 text-sm text-text-secondary">
      This role has no {kinds} question for this to read.{" "}
      <Link href={`/admin/roles/${roleId}/questions`} className="font-medium text-primary-ink underline-offset-4 hover:underline">
        Add one in Questions
      </Link>
      .
    </p>
  );
}

/** ConfigFor maps each kind to the config shape it owns. */
interface ConfigFor {
  keyword_match: KeywordConfig;
  experience_years: ExperienceConfig;
  answer_depth: DepthConfig;
  boolean_all: BooleanConfig;
}

/**
 * The registry, ordered as the builder offers the kinds. The mapped type
 * makes the compiler check that every kind has an entry, and that each
 * entry's editor, checks and conversions all agree on that kind's config.
 */
export const criterionKinds: { [K in CriterionKind]: KindDefinition<ConfigFor[K]> } = {
  keyword_match: keywordMatch,
  experience_years: experienceYears,
  answer_depth: answerDepth,
  boolean_all: booleanAll,
};

/**
 * kindOf returns a kind's definition with its config type widened. The
 * registry is typed per kind, but a criterion in the form holds a config of
 * whichever kind it has right now; this is the one place that bridges the
 * two, rather than a cast at every use.
 */
export function kindOf(kind: CriterionKind): KindDefinition<CriterionConfig> {
  return criterionKinds[kind] as unknown as KindDefinition<CriterionConfig>;
}

/** questionKindLabel names a question kind for a sentence. */
export const questionKindLabel: Record<QuestionKind, string> = {
  text: "written",
  number: "number",
  boolean: "yes/no",
  choice: "choice",
};
