// Formatting for display. One formatter instance each: constructing an
// Intl formatter is far slower than calling one, and these run per table cell.

const dateFormat = new Intl.DateTimeFormat("en-GB", { day: "numeric", month: "short", year: "numeric" });
const numberFormat = new Intl.NumberFormat("en-GB");

/** formatDate renders an ISO timestamp as "12 Nov 2026". */
export function formatDate(iso: string): string {
  return dateFormat.format(new Date(iso));
}

/** formatNumber renders 1500 as "1,500". */
export function formatNumber(n: number): string {
  return numberFormat.format(n);
}

/**
 * employmentTypes are the values the role form offers. The API stores the
 * field as free text, so a value outside this list (from an older client, or
 * Swagger) is still shown, as it was written.
 */
export const employmentTypes = [
  { value: "full_time", label: "Full-time" },
  { value: "part_time", label: "Part-time" },
  { value: "contract", label: "Contract" },
  { value: "internship", label: "Internship" },
  { value: "temporary", label: "Temporary" },
] as const;

/** employmentTypeLabel renders "full_time" as "Full-time". */
export function employmentTypeLabel(value: string): string {
  return employmentTypes.find((t) => t.value === value)?.label ?? value;
}

const moneyFormat = new Intl.NumberFormat("en-GB", { maximumFractionDigits: 0 });

/** salaryRange renders "45,000 – 60,000", "from 45,000", "up to 60,000", or null when neither is set. */
export function salaryRange(min: number | null, max: number | null): string | null {
  if (min !== null && max !== null) return `${moneyFormat.format(min)} – ${moneyFormat.format(max)}`;
  if (min !== null) return `from ${moneyFormat.format(min)}`;
  if (max !== null) return `up to ${moneyFormat.format(max)}`;
  return null;
}

/** placesLeft says how many vacancies are still open, in words. */
export function placesLeft(n: number): string {
  if (n <= 0) return "All places offered";
  return n === 1 ? "1 place left" : `${n} places left`;
}
