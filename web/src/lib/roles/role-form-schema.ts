import { z } from "zod";

import type { JobPost, JobPostInput } from "@/lib/api/types";

/** The longest title the API accepts, in characters (domain.MaxJobTitleLength). */
export const MAX_TITLE_LENGTH = 200;

// An HTML input's value is always a string, so every field starts as one.
// Numeric fields are checked as text, then transformed into numbers, so the
// schema has a different input type (what the form holds) and output type
// (what is sent).
const optionalAmount = z
  .string()
  .trim()
  // Mirrors domain.ParseMoney: a non-negative amount with at most two
  // decimal places, or empty for "not stated".
  .regex(/^(\d+(\.\d{1,2})?)?$/, "Enter an amount like 85000 or 85000.50")
  .transform((v) => (v === "" ? undefined : Number(v)));

/**
 * roleFormSchema validates the role form with the same rules the server
 * applies in domain.validateJobPost, so the user hears about a problem as
 * they make it rather than after a round trip.
 */
export const roleFormSchema = z
  .object({
    title: z
      .string()
      .trim()
      .min(1, "Give the role a title")
      .max(MAX_TITLE_LENGTH, `Keep the title to ${MAX_TITLE_LENGTH} characters or fewer`),
    description: z.string().trim(),
    employment_type: z.string(),
    location: z.string().trim(),
    vacancies: z
      .string()
      .trim()
      .regex(/^\d+$/, "Enter a whole number")
      .transform(Number)
      // pipe hands the transformed value to a second schema, so the number
      // rules run on a number rather than on its text.
      .pipe(z.number().int().min(1, "There must be at least 1 vacancy").max(2_147_483_647, "That is too many vacancies")),
    salary_min: optionalAmount,
    salary_max: optionalAmount,
    // "" or the yyyy-mm-dd an <input type="date"> produces.
    closes_at: z.string().regex(/^(\d{4}-\d{2}-\d{2})?$/, "Enter a date"),
  })
  // The cross-field rule. A field-level schema sees only its own value, so a
  // rule about two fields goes on the object, which sees them all.
  .refine((v) => v.salary_min === undefined || v.salary_max === undefined || v.salary_min <= v.salary_max, {
    message: "The minimum must not be more than the maximum",
    // Reported against salary_min, the field the server names for the same
    // problem, so a client error and a server error land in the same place.
    path: ["salary_min"],
    // By default the refine runs even when a salary field has already failed,
    // and then compares half-parsed data ("abc" <= 5). Run it only once both
    // amounts are valid numbers.
    when: (payload) => !payload.issues.some((i) => i.path?.[0] === "salary_min" || i.path?.[0] === "salary_max"),
  });

/** RoleFormValues is what the form holds: every field as text. Derived, never declared. */
export type RoleFormValues = z.input<typeof roleFormSchema>;

/** RoleFormOutput is what validation produces: numbers as numbers, blanks as undefined. */
export type RoleFormOutput = z.infer<typeof roleFormSchema>;

/** The form's field names, for mapping server errors onto them. */
export const roleFormFields = Object.keys(roleFormSchema.shape) as (keyof RoleFormValues)[];

export const emptyRoleForm: RoleFormValues = {
  title: "",
  description: "",
  employment_type: "",
  location: "",
  vacancies: "1",
  salary_min: "",
  salary_max: "",
  closes_at: "",
};

/** toDateInput turns an ISO timestamp into the yyyy-mm-dd a date input shows, in local time. */
function toDateInput(iso: string): string {
  const d = new Date(iso);
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}

/** fromDateInput turns yyyy-mm-dd into the end of that day, local time, as ISO. */
function fromDateInput(date: string): string {
  // No "Z": parsed as local time, so "closes 31 December" means the end of
  // 31 December where the admin is, not in UTC.
  return new Date(`${date}T23:59:59`).toISOString();
}

/** roleFormValuesFrom fills the form from a stored post. */
export function roleFormValuesFrom(post: JobPost): RoleFormValues {
  return {
    title: post.title,
    description: post.description ?? "",
    employment_type: post.employment_type ?? "",
    location: post.location ?? "",
    vacancies: String(post.vacancies),
    salary_min: post.salary_min === null ? "" : String(post.salary_min),
    salary_max: post.salary_max === null ? "" : String(post.salary_max),
    closes_at: post.closes_at ? toDateInput(post.closes_at) : "",
  };
}

/**
 * toJobPostInput builds the request body. With `only`, just those fields are
 * included: an edit sends what changed, as PATCH expects.
 */
export function toJobPostInput(v: RoleFormOutput, only?: Set<string>): JobPostInput {
  const include = (field: keyof RoleFormOutput) => !only || only.has(field);
  const body: JobPostInput = {};

  if (include("title")) body.title = v.title;
  if (include("vacancies")) body.vacancies = v.vacancies;

  // On create a blank text field is simply left out. On an edit it is sent as
  // "", which is how a description or location is emptied.
  for (const field of ["description", "employment_type", "location"] as const) {
    if (include(field) && (only || v[field] !== "")) body[field] = v[field];
  }

  // A blank amount or date is never sent: PATCH reads a missing key and a
  // null alike as "keep the stored value", so neither can be cleared.
  if (include("salary_min") && v.salary_min !== undefined) body.salary_min = v.salary_min;
  if (include("salary_max") && v.salary_max !== undefined) body.salary_max = v.salary_max;
  if (include("closes_at") && v.closes_at !== "") body.closes_at = fromDateInput(v.closes_at);

  return body;
}
