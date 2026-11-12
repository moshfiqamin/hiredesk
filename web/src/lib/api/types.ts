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

export type ApplicationStatus = "submitted" | "shortlisted" | "interview" | "offer" | "rejected";

export interface Assignee {
  id: string;
  name: string;
}

/** ApplicationListItem is one row of GET /v1/applications. */
export interface ApplicationListItem {
  id: string;
  job_post_id: string;
  candidate_id: string;
  status: ApplicationStatus;
  cv_url: string;
  submitted_at: string;
  updated_at: string;
  candidate_name: string;
  job_post_title: string;
  /** Absent for candidates; percentage null when not scored yet. */
  score?: { percentage: number | null };
  /** Admins only. */
  assignees?: Assignee[];
}

export interface ApplicationPage {
  data: ApplicationListItem[];
  next_cursor: string | null;
  total: number;
}

/** SearchEntry is one match of GET /v1/job-posts/:id/applications/search. */
export interface SearchEntry {
  application_id: string;
  candidate_id: string;
  candidate_name: string;
  status: ApplicationStatus;
  submitted_at: string;
  relevance: number;
  percentage: number | null;
  assignees: Assignee[];
}

export interface SearchPage {
  job_post_id: string;
  query: string;
  total: number;
  limit: number;
  offset: number;
  next_offset: number | null;
  data: SearchEntry[];
}

export interface Answer {
  id: string;
  question_id: string;
  answer: string;
  created_at: string;
}

export interface ApplicationEvent {
  id: string;
  actor_id: string | null;
  actor_name: string | null;
  type: string;
  payload: Record<string, unknown>;
  created_at: string;
}

/** ApplicationDetail is GET /v1/applications/:id. */
export interface ApplicationDetail {
  id: string;
  job_post_id: string;
  candidate_id: string;
  status: ApplicationStatus;
  cv_url: string;
  submitted_at: string;
  updated_at: string;
  answers: Answer[];
  events: ApplicationEvent[];
  /** Every review for an admin, only their own for an interviewer, absent for the candidate. */
  reviews?: Review[];
  /** The statuses PATCH .../status will accept next, from the server's own table. */
  allowed_next_statuses: ApplicationStatus[];
}

export interface Review {
  id: string;
  application_id: string;
  interviewer_id: string;
  interviewer_name: string;
  /** 1 to 10. */
  mark: number;
  notes: string | null;
  created_at: string;
  updated_at: string;
}

/** Assignment is one row of GET /v1/applications/:id/assignments. */
export interface Assignment {
  application_id: string;
  interviewer_id: string;
  interviewer_name: string;
  interviewer_email: string;
  assigned_by: string | null;
  assigned_by_name: string | null;
  assigned_at: string;
  reviewed: boolean;
}

/** RankingEntry is one row of GET /v1/job-posts/:id/ranking; score fields are null when unscored. */
export interface RankingEntry {
  rank: number;
  application_id: string;
  candidate_id: string;
  candidate_name: string;
  status: ApplicationStatus;
  submitted_at: string;
  total_earned: number | null;
  total_available: number | null;
  percentage: number | null;
  rubric_version: number | null;
  breakdown: { criterion: string; earned: number; available: number }[];
}

export interface Ranking {
  job_post_id: string;
  data: RankingEntry[];
}

export interface ScoreEntry {
  criterion: string;
  kind: CriterionKind;
  earned: number;
  available: number;
  detail: string;
  evidence: { question: string; answer_id: string; excerpt: string } | null;
}

export interface ScoreBreakdown {
  application_id: string;
  total_earned: number;
  total_available: number;
  percentage: number;
  rubric_version: number;
  breakdown: ScoreEntry[];
}
