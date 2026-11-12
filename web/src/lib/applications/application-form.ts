import { z } from "zod";

import { ApiError } from "@/lib/api/errors";
import type { AnswerProblem, Question } from "@/lib/api/types";

/**
 * ApplicationValues is the application form as it holds its data. Every
 * answer is a string, keyed by question ID, exactly as the API receives it:
 * a number is "4", a yes/no is "true" or "false", "" is unanswered. One
 * shape for every kind keeps the form state a plain record; the kind decides
 * how each string is edited and checked, not how it is stored.
 */
export interface ApplicationValues {
  cv_url: string;
  answers: Record<string, string>;
}

// Go counts a string's length in runes (Unicode code points). A JS string's
// .length counts UTF-16 units, which is 2 for an emoji, so the spread into
// code points matches the server where .length would not.
export const charCount = (s: string) => [...s.trim()].length;

const MAX_CV_URL_BYTES = 2048;

/** cvUrlProblem mirrors domain.checkCVURL: what is wrong with a CV link, or null. */
export function cvUrlProblem(raw: string): string | null {
  const value = raw.trim();
  if (value === "") return "Add a link to your CV.";
  // The server's limit is in bytes; TextEncoder gives the UTF-8 length.
  if (new TextEncoder().encode(value).length > MAX_CV_URL_BYTES) return "That link is too long; the limit is 2,048 characters.";
  try {
    const url = new URL(value);
    if ((url.protocol === "http:" || url.protocol === "https:") && url.host) return null;
  } catch {
    // new URL throws on anything that is not an absolute URL; fall through.
  }
  return "Use a full link starting with https://, for example https://example.com/cv.pdf";
}

/**
 * answerProblem mirrors domain.ValidateApplication for one question. A blank
 * answer counts as no answer, as on the server: it fails a required question
 * and is fine for an optional one.
 */
export function answerProblem(q: Question, raw: string): string | null {
  const text = raw.trim();
  if (text === "") return q.required ? "This question needs an answer." : null;

  switch (q.kind) {
    case "number": {
      // Number("") is 0 and Number("1e3") is 1000; the blank case is handled
      // above, and the server's ParseFloat accepts exponents too.
      const n = Number(text);
      return Number.isFinite(n) ? null : "Enter a number, such as 3 or 2.5.";
    }
    case "boolean":
      return text === "true" || text === "false" ? null : "Choose yes or no.";
    case "choice":
      return q.choices.some((c) => c.toLowerCase() === text.toLowerCase()) ? null : "Choose one of the options.";
    case "text": {
      const got = charCount(text);
      return got < q.min_length ? `Write at least ${q.min_length} characters (${got} so far).` : null;
    }
  }
}

/**
 * applicationSchema builds the form's Zod schema from the role's questions.
 * The shape is only known at runtime, so the schema is built per form with
 * superRefine, reusing the two functions above: one copy of each rule.
 */
export function applicationSchema(questions: Question[]) {
  return z
    .object({ cv_url: z.string(), answers: z.record(z.string(), z.string()) })
    .superRefine((values, ctx) => {
      const cv = cvUrlProblem(values.cv_url);
      if (cv) ctx.addIssue({ code: "custom", path: ["cv_url"], message: cv });
      for (const q of questions) {
        const problem = answerProblem(q, values.answers[q.id] ?? "");
        if (problem) ctx.addIssue({ code: "custom", path: ["answers", q.id], message: problem });
      }
    });
}

/** emptyValues is a blank form for these questions. */
export function emptyValues(questions: Question[]): ApplicationValues {
  return { cv_url: "", answers: Object.fromEntries(questions.map((q) => [q.id, ""])) };
}

/** toSubmission is the request body: trimmed, with blank optional answers left out, as the server would drop them. */
export function toSubmission(values: ApplicationValues, questions: Question[]) {
  return {
    cv_url: values.cv_url.trim(),
    answers: questions
      .map((q) => ({ question_id: q.id, answer: (values.answers[q.id] ?? "").trim() }))
      .filter((a) => a.answer !== ""),
  };
}

/**
 * submissionFieldErrors maps a 422 from the submit endpoint onto form paths:
 * {"cv_url": "...", "answers": [{"question_id", "code", ...}]} becomes
 * {"cv_url": "...", "answers.<id>": "..."}. Each code gets the same words
 * the client-side check would have used.
 */
export function submissionFieldErrors(error: unknown, questions: Question[]): Record<string, string> {
  if (!(error instanceof ApiError) || error.code !== "validation_failed") return {};
  const out: Record<string, string> = {};
  if (typeof error.details.cv_url === "string") out.cv_url = `The CV link ${error.details.cv_url}.`;
  const byId = new Map(questions.map((q) => [q.id, q]));
  for (const p of (error.details.answers as AnswerProblem[] | undefined) ?? []) {
    const q = byId.get(p.question_id);
    if (!q) continue; // unknown_question cannot come from this form, which only sends its own questions.
    out[`answers.${q.id}`] =
      p.code === "too_short"
        ? `Write at least ${p.min_length} characters (${p.got} so far).`
        : p.code === "required"
          ? "This question needs an answer."
          : `This answer ${p.message}.`;
  }
  return out;
}
