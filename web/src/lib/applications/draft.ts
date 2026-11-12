import type { Question, QuestionKind } from "@/lib/api/types";

import type { ApplicationValues } from "./application-form";

/**
 * A draft is what the form saves to localStorage as the candidate types.
 * Each answer is stored with the kind and prompt of the question it
 * answered, so that a draft opened against a form that has changed since can
 * tell which answers still fit.
 */
interface StoredDraft {
  version: 1;
  savedAt: string;
  cv_url: string;
  answers: Record<string, { kind: QuestionKind; prompt: string; value: string }>;
}

// Keyed by user and role, so two candidates sharing a browser never see each
// other's drafts, and one candidate's drafts for two roles stay apart. The
// version in the key lets a future shape be read without mistaking an old one.
const keyFor = (userId: string, postId: string) => `hiredesk.draft.v1:${userId}:${postId}`;

/** RestoredDraft is a saved draft fitted to the current form, with what did not fit. */
export interface RestoredDraft {
  values: ApplicationValues;
  savedAt: string;
  /** Prompts of saved answers that no longer fit any question, and were dropped. */
  dropped: string[];
  /** Prompts of questions reworded since the save; their answers were kept. */
  reworded: string[];
}

/**
 * loadDraft reads the saved draft and fits it to the current questions:
 *
 *   - same question ID and same kind: restored;
 *   - same ID, same kind, different prompt: restored, and listed as reworded
 *     so the candidate rereads it;
 *   - same ID but a different kind (a written answer where a number is now
 *     wanted), or a choice that is no longer offered: dropped;
 *   - a saved answer to a question that is gone: dropped.
 *
 * Questions added since start blank. Returns null when there is no usable
 * draft: none saved, storage blocked, or unreadable JSON.
 */
export function loadDraft(userId: string, postId: string, questions: Question[]): RestoredDraft | null {
  let stored: StoredDraft;
  try {
    const raw = window.localStorage.getItem(keyFor(userId, postId));
    if (!raw) return null;
    stored = JSON.parse(raw) as StoredDraft;
    if (stored.version !== 1 || typeof stored.answers !== "object") return null;
  } catch {
    return null;
  }

  const values: ApplicationValues = { cv_url: stored.cv_url ?? "", answers: {} };
  const reworded: string[] = [];
  const used = new Set<string>();

  for (const q of questions) {
    const saved = stored.answers[q.id];
    values.answers[q.id] = "";
    if (!saved) continue;
    used.add(q.id);
    const fits =
      saved.kind === q.kind && (q.kind !== "choice" || q.choices.some((c) => c.toLowerCase() === saved.value.toLowerCase()));
    if (!fits) continue;
    values.answers[q.id] = saved.value;
    if (saved.prompt !== q.prompt && saved.value.trim() !== "") reworded.push(q.prompt);
  }

  const dropped = Object.entries(stored.answers)
    .filter(([id, saved]) => saved.value.trim() !== "" && (!used.has(id) || values.answers[id] === ""))
    .map(([, saved]) => saved.prompt);

  return { values, savedAt: stored.savedAt, dropped, reworded };
}

/** saveDraft stores the form. It returns false when storage is unavailable (private mode, full quota). */
export function saveDraft(userId: string, postId: string, questions: Question[], values: ApplicationValues): boolean {
  const draft: StoredDraft = {
    version: 1,
    savedAt: new Date().toISOString(),
    cv_url: values.cv_url,
    answers: Object.fromEntries(
      questions.map((q) => [q.id, { kind: q.kind, prompt: q.prompt, value: values.answers[q.id] ?? "" }]),
    ),
  };
  try {
    window.localStorage.setItem(keyFor(userId, postId), JSON.stringify(draft));
    return true;
  } catch {
    return false;
  }
}

/** clearDraft removes the draft once the application is submitted. */
export function clearDraft(userId: string, postId: string) {
  try {
    window.localStorage.removeItem(keyFor(userId, postId));
  } catch {
    // Nothing to do: a draft that cannot be removed could not have been saved.
  }
}
