"use client";

import { XIcon } from "lucide-react";
import { useState, type KeyboardEvent } from "react";

import { Input } from "@/components/ui/input";

/**
 * KeywordInput edits a list of keywords as removable tags. Enter or a comma
 * adds what was typed; Backspace in an empty box removes the last tag.
 */
export function KeywordInput({
  id,
  value,
  onChange,
  invalid,
  describedBy,
}: {
  id: string;
  value: string[];
  onChange: (keywords: string[]) => void;
  invalid?: boolean;
  describedBy?: string;
}) {
  const [draft, setDraft] = useState("");
  const [notice, setNotice] = useState("");

  function add(raw: string) {
    // A paste of "go, gin, postgres" adds three.
    const incoming = raw
      .split(",")
      .map((k) => k.trim())
      .filter(Boolean);
    if (incoming.length === 0) return;

    const next = [...value];
    const skipped: string[] = [];
    for (const keyword of incoming) {
      // Case-insensitive, as the engine matches them.
      if (next.some((k) => k.toLowerCase() === keyword.toLowerCase())) skipped.push(keyword);
      else next.push(keyword);
    }
    onChange(next);
    setDraft("");
    setNotice(
      skipped.length > 0
        ? `${skipped.join(", ")} ${skipped.length === 1 ? "is" : "are"} already in the list.`
        : `Added ${incoming.join(", ")}.`,
    );
  }

  function remove(index: number) {
    const keyword = value[index];
    onChange(value.filter((_, i) => i !== index));
    setNotice(`Removed ${keyword}.`);
  }

  function onKeyDown(event: KeyboardEvent<HTMLInputElement>) {
    if (event.key === "Enter" || event.key === ",") {
      // Enter would otherwise submit the whole rubric form.
      event.preventDefault();
      add(draft);
    } else if (event.key === "Backspace" && draft === "" && value.length > 0) {
      remove(value.length - 1);
    }
  }

  return (
    <div className="flex flex-col gap-2">
      {value.length > 0 && (
        <ul aria-label="Keywords" className="flex flex-wrap gap-1.5">
          {value.map((keyword, i) => (
            <li
              key={keyword.toLowerCase()}
              className="inline-flex h-6 items-center gap-1 rounded-md border bg-surface-sunken pr-0.5 pl-2 font-mono text-xs"
            >
              {keyword}
              <button
                type="button"
                aria-label={`Remove keyword ${keyword}`}
                onClick={() => remove(i)}
                className="flex size-5 items-center justify-center rounded text-muted-foreground outline-none hover:bg-surface-hover hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring/50"
              >
                <XIcon className="size-3" aria-hidden="true" />
              </button>
            </li>
          ))}
        </ul>
      )}
      <Input
        id={id}
        value={draft}
        placeholder={value.length ? "Add another keyword" : "Type a keyword and press Enter"}
        aria-invalid={invalid}
        aria-describedby={describedBy}
        onChange={(e) => setDraft(e.target.value)}
        onKeyDown={onKeyDown}
        // Typing a keyword and tabbing away should not silently lose it.
        onBlur={() => add(draft)}
      />
      <p className="sr-only" role="status" aria-live="polite">
        {notice}
      </p>
    </div>
  );
}
