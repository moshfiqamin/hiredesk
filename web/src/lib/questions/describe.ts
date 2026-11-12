import type { Question } from "@/lib/api/types";

/** describeQuestion says, in a few words, what kind of answer a question expects. */
export function describeQuestion(q: Question): string {
  switch (q.kind) {
    case "text":
      return q.min_length > 0 ? `Written answer, at least ${q.min_length} characters` : "Written answer";
    case "number":
      return "A number";
    case "boolean":
      return "Yes or no";
    case "choice":
      return `One of: ${q.choices.join(", ")}`;
  }
}
