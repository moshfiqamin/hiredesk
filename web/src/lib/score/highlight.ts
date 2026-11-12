/** Segment is one run of an excerpt: plain text, or a keyword that matched. */
export interface Segment {
  text: string;
  match: boolean;
}

/**
 * matchedKeywords reads the keywords the engine reported as matched from a
 * keyword_match detail string such as "matched 2 of 5 keywords: go, gin".
 * The server's list is used rather than the rubric's, so only words that
 * actually earned points are highlighted.
 */
export function matchedKeywords(detail: string): string[] {
  const colon = detail.indexOf(": ");
  if (!detail.startsWith("matched ") || colon < 0) return [];
  return detail
    .slice(colon + 2)
    .split(", ")
    .map((k) => k.trim())
    .filter(Boolean);
}

// Escapes regexp metacharacters, so a keyword such as "c++" is matched literally.
const escapeRegExp = (s: string) => s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");

// The same word characters as the scoring engine (internal/scoring/keyword.go):
// any letter or digit in any script, and underscore.
const WORD = "\\p{L}\\p{N}_";

/**
 * highlight splits text into segments, marking each whole-word,
 * case-insensitive occurrence of a keyword. It returns data, never HTML: the
 * caller renders each segment as a React text child, which React escapes.
 */
export function highlight(text: string, keywords: string[]): Segment[] {
  if (keywords.length === 0) return [{ text, match: false }];

  // Longest first, so "golang" wins over "go" where both could start.
  const alternatives = [...keywords].sort((a, b) => b.length - a.length).map(escapeRegExp);
  // Lookbehind and lookahead keep the edge characters out of the match;
  // the "u" flag makes \p{L} mean Unicode letters.
  const re = new RegExp(`(?<![${WORD}])(?:${alternatives.join("|")})(?![${WORD}])`, "giu");

  const out: Segment[] = [];
  let last = 0;
  for (const m of text.matchAll(re)) {
    const start = m.index;
    if (start > last) out.push({ text: text.slice(last, start), match: false });
    out.push({ text: m[0], match: true });
    last = start + m[0].length;
  }
  if (last < text.length) out.push({ text: text.slice(last), match: false });
  return out;
}

/**
 * zeroReason explains a criterion that earned nothing, in words an admin can
 * act on. It returns null for a criterion that earned something.
 */
export function zeroReason(earned: number, detail: string): string | null {
  if (earned > 0) return null;
  if (detail === "no answer provided") return "No answer was provided to the question this criterion scores.";
  if (detail.includes("at or below the minimum")) return `Below the minimum: ${detail}.`;
  if (detail.startsWith("matched 0 of")) return `None of the expected keywords appear in the answer (${detail}).`;
  if (detail.startsWith("could not read")) return `The answer could not be read as a number: ${detail}.`;
  return `Scored zero: ${detail}.`;
}
