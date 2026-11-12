import type { QuestionDraft } from "@/lib/questions/schema";

/**
 * FormPreview renders the questions as a candidate will see them. It is
 * inert: it looks like the real form but takes no focus and no input, so it
 * never becomes a second set of controls for keyboard and screen-reader users.
 */
export function FormPreview({ questions, title }: { questions: QuestionDraft[]; title: string }) {
  return (
    <section aria-labelledby="preview-heading" className="rounded-lg border bg-surface">
      <header className="border-b px-4 py-2.5">
        <h2 id="preview-heading" className="text-sm font-medium">
          What candidates see
        </h2>
      </header>
      {/* inert removes everything inside from the tab order and the
          accessibility tree; the heading above stays readable. */}
      <div inert className="flex flex-col gap-5 p-4">
        <div>
          <p className="text-xs text-muted-foreground">Applying for</p>
          <p className="font-medium">{title}</p>
        </div>
        {questions.length === 0 && <p className="text-sm text-muted-foreground">No questions yet.</p>}
        {questions.map((q, i) => (
          <PreviewQuestion key={i} question={q} number={i + 1} />
        ))}
        <div className="h-8 w-28 rounded-lg bg-primary/40" />
      </div>
    </section>
  );
}

function PreviewQuestion({ question: q, number }: { question: QuestionDraft; number: number }) {
  const prompt = q.prompt.trim() || "Untitled question";
  const minLength = Number(q.min_length) || 0;
  const options = q.choices.map((c) => c.value.trim()).filter(Boolean);

  return (
    <div className="flex flex-col gap-1.5 text-sm">
      <p className="font-medium">
        <span className="tabular text-muted-foreground">{number}. </span>
        {prompt}
        {q.required ? (
          <span className="text-destructive"> *</span>
        ) : (
          <span className="font-normal text-muted-foreground"> (optional)</span>
        )}
      </p>
      {q.kind === "text" && (
        <>
          <div className="h-16 rounded-lg border border-input bg-transparent" />
          {minLength > 0 && <p className="text-xs text-muted-foreground">At least {minLength} characters</p>}
        </>
      )}
      {q.kind === "number" && <div className="h-8 w-32 rounded-lg border border-input" />}
      {q.kind === "boolean" && (
        <div className="flex gap-4">
          {["Yes", "No"].map((o) => (
            <span key={o} className="flex items-center gap-1.5">
              <span className="size-4 rounded-full border border-input" />
              {o}
            </span>
          ))}
        </div>
      )}
      {q.kind === "choice" && (
        <div className="flex flex-col gap-1.5">
          {options.length === 0 && <p className="text-xs text-muted-foreground">No options yet.</p>}
          {options.map((o, i) => (
            <span key={i} className="flex items-center gap-1.5">
              <span className="size-4 shrink-0 rounded-full border border-input" />
              {o}
            </span>
          ))}
        </div>
      )}
    </div>
  );
}
