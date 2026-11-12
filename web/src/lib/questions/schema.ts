import { z } from "zod";

import type { Question, QuestionKind } from "@/lib/api/types";

// The server's limits, from internal/domain/question.go.
export const MAX_PROMPT_LENGTH = 1000;
export const MAX_CHOICES = 50;
export const MAX_CHOICE_LENGTH = 200;
export const MAX_MIN_LENGTH = 10_000;

export const questionKinds: { value: QuestionKind; label: string; hint: string }[] = [
  { value: "text", label: "Text", hint: "A written answer, with an optional minimum length." },
  { value: "number", label: "Number", hint: "A number, such as years of experience." },
  { value: "boolean", label: "Yes / no", hint: "A single yes or no." },
  { value: "choice", label: "Choice", hint: "One option from a list you write." },
];

// Each option is an object, not a bare string: useFieldArray needs objects so
// it can attach a stable key to each row.
const choiceSchema = z.object({
  value: z.string().trim().min(1, "Write the option or remove it").max(MAX_CHOICE_LENGTH, `At most ${MAX_CHOICE_LENGTH} characters`),
});

/**
 * questionSchema is one question as the builder holds it. Every field exists
 * for every kind, so switching kind is a value change rather than a change of
 * shape; the kind decides which fields must be filled and which must be
 * empty.
 */
export const questionSchema = z
  .object({
    // Present once the question exists on the server; absent while it is new.
    // Not called `id`: useFieldArray adds its own `id` to every row.
    questionId: z.string().optional(),
    prompt: z
      .string()
      .trim()
      .min(1, "Write the question")
      .max(MAX_PROMPT_LENGTH, `At most ${MAX_PROMPT_LENGTH} characters`),
    kind: z.enum(["text", "number", "boolean", "choice"]),
    required: z.boolean(),
    min_length: z.string(),
    choices: z.array(choiceSchema),
  })
  // superRefine is refine with a context for reporting several issues, each
  // at its own path. The rules depend on the kind, so they cannot sit on the
  // individual fields.
  .superRefine((q, ctx) => {
    if (q.kind === "text") {
      const n = Number(q.min_length);
      if (!/^\d+$/.test(q.min_length.trim()) || n > MAX_MIN_LENGTH) {
        ctx.addIssue({ code: "custom", path: ["min_length"], message: `A whole number from 0 to ${MAX_MIN_LENGTH}` });
      }
    } else if (q.min_length.trim() !== "" && q.min_length.trim() !== "0") {
      // Only reachable if a kind change forgot to clear it: the server would
      // refuse it ("applies only to text questions").
      ctx.addIssue({ code: "custom", path: ["min_length"], message: "Only text questions have a minimum length" });
    }

    if (q.kind === "choice") {
      if (q.choices.length === 0) {
        ctx.addIssue({ code: "custom", path: ["choices"], message: "Add at least one option" });
      } else if (q.choices.length > MAX_CHOICES) {
        ctx.addIssue({ code: "custom", path: ["choices"], message: `At most ${MAX_CHOICES} options` });
      }
      // Case-insensitive, as the server compares them: an answer is stored as
      // the chosen text, and "Remote" and "remote" could not be told apart.
      const seen = new Set<string>();
      q.choices.forEach((c, i) => {
        const key = c.value.trim().toLowerCase();
        if (key && seen.has(key)) {
          ctx.addIssue({ code: "custom", path: ["choices", i, "value"], message: "Already in the list" });
        }
        seen.add(key);
      });
    } else if (q.choices.length > 0) {
      ctx.addIssue({ code: "custom", path: ["choices"], message: "Only choice questions have options" });
    }
  });

export const questionFormSchema = z.object({ questions: z.array(questionSchema) });

/** QuestionDraft is one question in the builder. Derived from the schema. */
export type QuestionDraft = z.infer<typeof questionSchema>;
export type QuestionFormValues = z.infer<typeof questionFormSchema>;

/** draftFrom converts a stored question into the builder's shape. */
export function draftFrom(q: Question): QuestionDraft {
  return {
    questionId: q.id,
    prompt: q.prompt,
    kind: q.kind,
    required: q.required,
    min_length: q.kind === "text" ? String(q.min_length) : "",
    choices: q.kind === "choice" ? q.choices.map((value) => ({ value })) : [],
  };
}

export const blankQuestion = (): QuestionDraft => ({
  prompt: "",
  kind: "text",
  required: true,
  min_length: "0",
  choices: [],
});

/**
 * fieldsForKind returns the kind-specific fields as they must be for `kind`:
 * a minimum length only for text, options only for choice. Applied on every
 * kind change, so a value that no longer applies cannot survive in the form
 * and reach the server.
 */
export function fieldsForKind(kind: QuestionKind): Pick<QuestionDraft, "min_length" | "choices"> {
  return { min_length: kind === "text" ? "0" : "", choices: [] };
}

/** The body of POST /v1/job-posts/:id/questions: only what the kind uses. */
export function createBody(q: QuestionDraft) {
  return {
    prompt: q.prompt.trim(),
    kind: q.kind,
    required: q.required,
    ...(q.kind === "text" ? { min_length: Number(q.min_length) } : {}),
    ...(q.kind === "choice" ? { choices: q.choices.map((c) => c.value.trim()) } : {}),
  };
}

/**
 * The body of PATCH /v1/job-posts/:id/questions/:qid. A PATCH is applied on
 * top of the stored question, so every kind-specific field is sent, cleared
 * where it does not apply: turning a choice question into text must send
 * `choices: []`, or the stored options would remain and the server would
 * refuse the result.
 */
export function patchBody(q: QuestionDraft) {
  return {
    prompt: q.prompt.trim(),
    kind: q.kind,
    required: q.required,
    min_length: q.kind === "text" ? Number(q.min_length) : 0,
    choices: q.kind === "choice" ? q.choices.map((c) => c.value.trim()) : [],
  };
}

/** sameQuestion reports whether a draft would change nothing on the server. */
export function sameQuestion(a: QuestionDraft, b: QuestionDraft): boolean {
  return JSON.stringify(patchBody(a)) === JSON.stringify(patchBody(b));
}
