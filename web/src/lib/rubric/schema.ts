import { z } from "zod";

import type { Criterion, CriterionInput, CriterionKind, Question } from "@/lib/api/types";

import { kindOf, questionKindLabel, type CriterionConfig } from "./kinds";

// The server's limits: domain.MaxRubricCriteria, MaxCriterionLabelLength,
// and scoring.RequiredWeightTotal.
export const MAX_CRITERIA = 50;
export const MAX_LABEL_LENGTH = 120;
export const REQUIRED_TOTAL = 100;

/**
 * toHundredths turns a weight as typed ("33.33") into a whole number of
 * hundredths (3333), or null if it is not a weight. Totals are summed in
 * hundredths because 0.1 + 0.2 is 0.30000000000000004 in floating point:
 * three weights of 33.33, 33.33 and 33.34 must add up to exactly 100.
 */
export function toHundredths(weight: string): number | null {
  const match = /^(\d{1,3})(?:\.(\d{1,2}))?$/.exec(weight.trim());
  if (!match) return null;
  return Number(match[1]) * 100 + Number((match[2] ?? "").padEnd(2, "0"));
}

/**
 * rubricFormSchema builds the schema for one role. It needs the role's
 * questions, because whether a criterion is valid depends on them: the
 * question it reads must be on this form, and of a kind it can score.
 */
export function rubricFormSchema(questions: Question[]) {
  const byId = new Map(questions.map((q) => [q.id, q]));

  const criterion = z
    .object({
      label: z.string().trim().min(1, "Name the criterion").max(MAX_LABEL_LENGTH, `At most ${MAX_LABEL_LENGTH} characters`),
      kind: z.enum(["keyword_match", "experience_years", "answer_depth", "boolean_all"]),
      weight: z
        .string()
        .refine((w) => toHundredths(w) !== null, "A number like 20 or 12.5, at most two decimals")
        .refine((w) => (toHundredths(w) ?? 0) <= REQUIRED_TOTAL * 100, "At most 100"),
      question_id: z.string(),
      // The config's shape depends on the kind, so its checks live with the
      // kind, in the registry, and are run below.
      config: z.custom<CriterionConfig>(),
    })
    .superRefine((c, ctx) => {
      const def = kindOf(c.kind);
      const allowed = def.questionKinds.map((k) => questionKindLabel[k]).join(" or ");

      if (def.needsQuestion) {
        const q = byId.get(c.question_id);
        if (!c.question_id) {
          ctx.addIssue({ code: "custom", path: ["question_id"], message: `Choose the ${allowed} question it scores` });
        } else if (!q) {
          ctx.addIssue({ code: "custom", path: ["question_id"], message: "That question is no longer on this role" });
        } else if (!def.questionKinds.includes(q.kind)) {
          ctx.addIssue({ code: "custom", path: ["question_id"], message: `${def.label} reads ${allowed} questions` });
        }
      }

      def.check(c.config, (path, message) => ctx.addIssue({ code: "custom", path: ["config", path], message }));
    });

  return z.object({
    criteria: z.array(criterion).min(1, "Add at least one criterion").max(MAX_CRITERIA, `At most ${MAX_CRITERIA} criteria`),
  });
}

export type RubricFormValues = z.infer<ReturnType<typeof rubricFormSchema>>;
export type CriterionDraft = RubricFormValues["criteria"][number];

/** blankCriterion starts a new criterion of the given kind, with that kind's empty config. */
export function blankCriterion(kind: CriterionKind = "keyword_match"): CriterionDraft {
  return { label: "", kind, weight: "", question_id: "", config: kindOf(kind).empty() };
}

export function draftFrom(c: Criterion): CriterionDraft {
  return {
    label: c.label,
    kind: c.kind,
    weight: String(c.weight),
    question_id: c.question_id ?? "",
    config: kindOf(c.kind).fromApi(c.config),
  };
}

/** toInput builds one criterion of the PUT body, sending only what its kind reads. */
export function toInput(c: CriterionDraft): CriterionInput {
  const def = kindOf(c.kind);
  return {
    label: c.label.trim(),
    kind: c.kind,
    weight: (toHundredths(c.weight) ?? 0) / 100,
    question_id: def.needsQuestion ? c.question_id : null,
    config: def.toApi(c.config),
  };
}
