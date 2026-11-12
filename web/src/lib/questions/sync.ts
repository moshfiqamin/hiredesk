import { api } from "@/lib/api/client";
import type { Question } from "@/lib/api/types";

import { createBody, draftFrom, patchBody, sameQuestion, type QuestionDraft } from "./schema";

/** QuestionSaveError says which question (by index in the builder) a failed request was for. */
export class QuestionSaveError extends Error {
  constructor(
    readonly index: number | null,
    readonly cause: unknown,
  ) {
    super(cause instanceof Error ? cause.message : "Could not save the form.");
  }
}

/** Baseline is what the server holds, as far as the builder knows: by ID, and in order. */
export interface Baseline {
  byId: Map<string, QuestionDraft>;
  order: string[];
}

export function baselineFrom(questions: Question[]): Baseline {
  return { byId: new Map(questions.map((q) => [q.id, draftFrom(q)])), order: questions.map((q) => q.id) };
}

/**
 * saveQuestions makes the server's form match the builder, with the fewest
 * requests: delete what was removed, create what is new, patch what changed,
 * and reorder once at the end if the order differs.
 *
 * The API has no endpoint that replaces a whole form at once, so this is
 * several requests, not one transaction. It updates `baseline` after each
 * request that succeeds, and `onCreated` records a new question's ID in the
 * builder, so if a later request fails, pressing Save again resumes from
 * there instead of repeating what already happened (which would create a
 * question twice).
 */
export async function saveQuestions(
  jobPostId: string,
  drafts: QuestionDraft[],
  baseline: Baseline,
  onCreated: (index: number, question: Question) => void,
): Promise<void> {
  const base = `/v1/job-posts/${jobPostId}/questions`;

  const kept = new Set(drafts.map((d) => d.questionId).filter(Boolean));
  for (const id of [...baseline.order]) {
    if (kept.has(id)) continue;
    try {
      await api(`${base}/${id}`, { method: "DELETE" });
    } catch (cause) {
      throw new QuestionSaveError(null, cause);
    }
    baseline.byId.delete(id);
    baseline.order = baseline.order.filter((x) => x !== id);
  }

  const wanted: string[] = [];
  for (const [index, draft] of drafts.entries()) {
    try {
      if (!draft.questionId) {
        // The server appends a new question after the last one.
        const created = await api<Question>(base, { method: "POST", body: createBody(draft) });
        baseline.byId.set(created.id, draftFrom(created));
        baseline.order = [...baseline.order, created.id];
        onCreated(index, created);
        wanted.push(created.id);
      } else {
        const stored = baseline.byId.get(draft.questionId);
        if (!stored || !sameQuestion(draft, stored)) {
          const updated = await api<Question>(`${base}/${draft.questionId}`, { method: "PATCH", body: patchBody(draft) });
          baseline.byId.set(updated.id, draftFrom(updated));
        }
        wanted.push(draft.questionId);
      }
    } catch (cause) {
      throw new QuestionSaveError(index, cause);
    }
  }

  if (wanted.join() !== baseline.order.join()) {
    try {
      await api(`${base}/order`, { method: "PUT", body: { question_ids: wanted } });
    } catch (cause) {
      throw new QuestionSaveError(null, cause);
    }
    baseline.order = wanted;
  }
}
