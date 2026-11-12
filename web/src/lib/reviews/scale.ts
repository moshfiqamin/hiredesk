/**
 * The review scale: ten marks in five anchored bands. Each band describes
 * observable evidence against the role's bar, not a feeling, so two
 * interviewers who saw the same interview land in the same band. Within a
 * band, the higher mark means "I would defend this without hesitation", the
 * lower "with a reservation".
 *
 * Keep in step with docs/notes/14-1-interviewer-side.md, which explains the
 * design.
 */
export interface MarkBand {
  marks: [number, number];
  label: string;
  meaning: string;
}

export const markBands: MarkBand[] = [
  { marks: [1, 2], label: "Clear no", meaning: "Could not show the core skills the role needs; gaps in the fundamentals." },
  { marks: [3, 4], label: "Lean no", meaning: "Some relevant skill, but gaps that would need significant support to close." },
  { marks: [5, 6], label: "Mixed", meaning: "Meets some of the bar and misses some; you could argue it either way." },
  { marks: [7, 8], label: "Lean yes", meaning: "Meets the bar for the role; any gaps are small and coachable." },
  { marks: [9, 10], label: "Strong yes", meaning: "Clearly above the bar; would raise the level of the team." },
];

/** bandFor returns the band a mark belongs to. */
export function bandFor(mark: number): MarkBand {
  return markBands.find((b) => mark >= b.marks[0] && mark <= b.marks[1]) ?? markBands[0];
}

/** confidence describes where a mark sits inside its band. */
export const confidence = (mark: number) => (mark % 2 === 0 ? "without reservation" : "with a reservation");

/** MAX_NOTES is the server's limit on review notes, in characters (domain.MaxReviewNotesChars). */
export const MAX_NOTES = 5000;
