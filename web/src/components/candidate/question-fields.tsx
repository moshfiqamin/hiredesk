"use client";

import type { ComponentType } from "react";

import { Input } from "@/components/ui/input";
import { NativeSelect } from "@/components/ui/native-select";
import { Textarea } from "@/components/ui/textarea";
import type { Question, QuestionKind } from "@/lib/api/types";
import { charCount } from "@/lib/applications/application-form";

/** QuestionFieldProps is what every kind's input receives. */
export interface QuestionFieldProps {
  question: Question;
  value: string;
  onChange: (value: string) => void;
  onBlur: () => void;
  /** The id the label points at, and the prefix for this field's other ids. */
  id: string;
  /** Ids of the hint and error, for aria-describedby. */
  describedBy: string | undefined;
  invalid: boolean;
}

function TextField({ question, value, onChange, onBlur, id, describedBy, invalid }: QuestionFieldProps) {
  const got = charCount(value);
  const min = question.min_length;
  return (
    <>
      <Textarea
        id={id}
        rows={5}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        onBlur={onBlur}
        required={question.required}
        aria-invalid={invalid || undefined}
        aria-describedby={[describedBy, min > 0 ? `${id}-count` : undefined].filter(Boolean).join(" ") || undefined}
      />
      {min > 0 && (
        // Visible on every keystroke but not a live region: a count read
        // aloud per letter would drown the screen reader. It is read with
        // the field (aria-describedby), and the error says it on submit.
        <p id={`${id}-count`} className="text-right font-mono text-xs tabular-nums text-muted-foreground">
          <span className={got >= min ? "text-foreground" : undefined}>{got}</span> / {min} characters minimum
          {got >= min && <span className="sr-only">, minimum reached</span>}
        </p>
      )}
    </>
  );
}

function NumberField({ question, value, onChange, onBlur, id, describedBy, invalid }: QuestionFieldProps) {
  return (
    // type="text" with inputMode="decimal" rather than type="number": the
    // number input silently changes value on a scroll wheel, accepts "e",
    // and reports "" for a half-typed "1." so the field can't show what
    // the person typed. The numeric keyboard still appears on phones.
    <Input
      id={id}
      inputMode="decimal"
      autoComplete="off"
      className="max-w-40"
      value={value}
      onChange={(e) => onChange(e.target.value)}
      onBlur={onBlur}
      required={question.required}
      aria-invalid={invalid || undefined}
      aria-describedby={describedBy}
    />
  );
}

/** RadioOptions is a native radio group: arrow keys move between options, Tab leaves the group. */
function RadioOptions({
  name,
  options,
  value,
  onChange,
  onBlur,
  describedBy,
  required,
}: {
  name: string;
  options: { value: string; label: string }[];
  value: string;
  onChange: (v: string) => void;
  onBlur: () => void;
  describedBy: string | undefined;
  required: boolean;
}) {
  return (
    // No role here: the form wraps these in a <fieldset> whose <legend> is
    // the question, which is how a radio group is named natively. A radio
    // cannot carry aria-invalid, so the error reaches it through
    // aria-describedby, and the group's border shows it visually.
    <div className="flex flex-wrap gap-2">
      {options.map((o) => (
        <label
          key={o.value}
          className="flex cursor-pointer items-center gap-2 rounded-md border border-border-field bg-surface px-3 py-2 text-sm has-checked:border-primary has-checked:bg-primary-soft has-checked:text-primary-ink has-focus-visible:ring-2 has-focus-visible:ring-ring"
        >
          <input
            type="radio"
            name={name}
            value={o.value}
            checked={value === o.value}
            onChange={() => onChange(o.value)}
            onBlur={onBlur}
            required={required}
            aria-describedby={describedBy}
            className="size-4 accent-[var(--primary)]"
          />
          {o.label}
        </label>
      ))}
    </div>
  );
}

function BooleanField({ question, value, onChange, onBlur, id, describedBy }: QuestionFieldProps) {
  return (
    <RadioOptions
      name={id}
      options={[
        { value: "true", label: "Yes" },
        { value: "false", label: "No" },
      ]}
      value={value}
      onChange={onChange}
      onBlur={onBlur}
      describedBy={describedBy}
      required={question.required}
    />
  );
}

function ChoiceField({ question, value, onChange, onBlur, id, describedBy, invalid }: QuestionFieldProps) {
  // Up to five options read best as buttons, all visible at once; more than
  // that becomes a long wall of them, so a select takes over.
  if (question.choices.length <= 5) {
    return (
      <RadioOptions
        name={id}
        options={question.choices.map((c) => ({ value: c, label: c }))}
        value={value}
        onChange={onChange}
        onBlur={onBlur}
        describedBy={describedBy}
        required={question.required}
      />
    );
  }
  return (
    <NativeSelect
      id={id}
      value={value}
      onChange={(e) => onChange(e.target.value)}
      onBlur={onBlur}
      required={question.required}
      aria-invalid={invalid || undefined}
      aria-describedby={describedBy}
      className="max-w-sm"
    >
      <option value="">Choose one…</option>
      {question.choices.map((c) => (
        <option key={c} value={c}>
          {c}
        </option>
      ))}
    </NativeSelect>
  );
}

/**
 * questionFields is the input for each question kind. A mapped type over
 * QuestionKind: if the API gains a fifth kind and types.ts is updated, this
 * object stops compiling until the new kind has an input. The form never
 * switches on kind itself.
 */
export const questionFields: { [K in QuestionKind]: ComponentType<QuestionFieldProps> } = {
  text: TextField,
  number: NumberField,
  boolean: BooleanField,
  choice: ChoiceField,
};

/** groupKinds are rendered as a fieldset with a legend, since they are several inputs, not one. */
export const isGroupKind = (q: Question) => q.kind === "boolean" || (q.kind === "choice" && q.choices.length <= 5);
