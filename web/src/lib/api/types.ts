// Response and request shapes of the HireDesk API, as documented in
// api/openapi.yaml. Field names stay snake_case, exactly as they arrive, so
// a field can be searched for across the Go code, the spec and this file.

export type Role = "admin" | "interviewer" | "candidate";

export interface User {
  id: string;
  email: string;
  full_name: string;
  role: Role;
  created_at: string;
}

export interface TokenPair {
  access_token: string;
  token_type: "Bearer";
  expires_in: number;
  refresh_token: string;
  user: User;
}

export type JobPostStatus = "draft" | "open" | "closed";

export type QuestionKind = "text" | "number" | "boolean" | "choice";

export interface Question {
  id: string;
  prompt: string;
  kind: QuestionKind;
  choices: string[];
  required: boolean;
  min_length: number;
  position: number;
}

/** JobPostFields are the columns shared by a single post and a list row. */
export interface JobPostFields {
  id: string;
  title: string;
  description: string | null;
  employment_type: string | null;
  location: string | null;
  vacancies: number;
  salary_min: number | null;
  salary_max: number | null;
  status: JobPostStatus;
  closes_at: string | null;
  created_by: string | null;
  created_at: string;
  updated_at: string;
}

/** JobPost is GET /v1/job-posts/:id: one post with its application form. */
export interface JobPost extends JobPostFields {
  questions: Question[];
}

/** JobPostSummary is one row of GET /v1/admin/job-posts. */
export interface JobPostSummary extends JobPostFields {
  application_count: number;
  remaining_vacancies: number;
}

/** JobPostInput is the body of POST /v1/job-posts and PATCH /v1/job-posts/:id. */
export interface JobPostInput {
  title?: string;
  description?: string;
  employment_type?: string;
  location?: string;
  vacancies?: number;
  salary_min?: number;
  salary_max?: number;
  closes_at?: string;
}

/** QuestionInput is the body of POST and PATCH on a post's questions. */
export interface QuestionInput {
  prompt?: string;
  kind?: QuestionKind;
  required?: boolean;
  min_length?: number;
  choices?: string[];
}

export type CriterionKind = "keyword_match" | "experience_years" | "answer_depth" | "boolean_all";

/** Criterion is one stored rubric criterion. config's shape depends on kind. */
export interface Criterion {
  id: string;
  label: string;
  kind: CriterionKind;
  weight: number;
  question_id: string | null;
  config: Record<string, unknown>;
}

/** Rubric is GET /v1/job-posts/:id/rubric. version is null before the first save. */
export interface Rubric {
  job_post_id: string;
  version: number | null;
  criteria: Criterion[];
}

/** ReplacedRubric is what PUT /v1/job-posts/:id/rubric returns. */
export interface ReplacedRubric extends Rubric {
  previous_version: number | null;
  applications_to_rescore: number;
}

export interface CriterionInput {
  label: string;
  kind: CriterionKind;
  weight: number;
  question_id: string | null;
  config: Record<string, unknown>;
}
