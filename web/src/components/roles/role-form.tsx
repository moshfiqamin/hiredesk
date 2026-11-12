"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { AlertTriangleIcon } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useForm, useWatch } from "react-hook-form";
import { toast } from "sonner";

import { Button, buttonVariants } from "@/components/ui/button";
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel, FieldLegend, FieldSet } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { NativeSelect } from "@/components/ui/native-select";
import { Textarea } from "@/components/ui/textarea";
import { useCreateJobPost, useUpdateJobPost } from "@/hooks/use-job-posts";
import { useUnsavedChangesWarning } from "@/hooks/use-unsaved-changes";
import type { JobPost } from "@/lib/api/types";
import { employmentTypes } from "@/lib/format";
import { applyServerErrors } from "@/lib/forms";
import {
  MAX_TITLE_LENGTH,
  emptyRoleForm,
  roleFormFields,
  roleFormSchema,
  roleFormValuesFrom,
  toJobPostInput,
  type RoleFormOutput,
  type RoleFormValues,
} from "@/lib/roles/role-form-schema";

/**
 * RoleForm creates a role, or edits one when given `post`. One component for
 * both: the fields, rules and error handling are identical, and only the
 * starting values, the request and the destination differ.
 */
export function RoleForm({ post }: { post?: JobPost }) {
  const router = useRouter();
  const editing = post !== undefined;
  const create = useCreateJobPost();
  // Hooks cannot be called conditionally, so the update hook always exists;
  // with no post it is simply never used.
  const update = useUpdateJobPost(post?.id ?? "");

  // Three type arguments: what the inputs hold, a context (unused), and what
  // handleSubmit receives after the resolver has transformed the values.
  const form = useForm<RoleFormValues, unknown, RoleFormOutput>({
    resolver: zodResolver(roleFormSchema),
    defaultValues: post ? roleFormValuesFrom(post) : emptyRoleForm,
    // Errors appear once a field has been left, then update on every
    // keystroke: no shouting while the user is still typing the first time.
    mode: "onTouched",
  });
  const {
    register,
    handleSubmit,
    setError,
    reset,
    control,
    formState: { errors, isValid, isDirty, isSubmitting, dirtyFields },
  } = form;

  useUnsavedChangesWarning(isDirty && !isSubmitting);

  async function onSubmit(values: RoleFormOutput) {
    try {
      if (editing) {
        // Only what changed: PATCH treats every key it receives as a change.
        const changed = new Set(Object.keys(dirtyFields));
        const saved = await update.mutateAsync(toJobPostInput(values, changed));
        // Resetting to the saved values makes the form clean, which turns the
        // unsaved-changes warning off before navigating.
        reset(roleFormValuesFrom(saved));
        toast.success("Changes saved");
        router.replace(`/admin/roles/${saved.id}`);
      } else {
        const created = await create.mutateAsync(toJobPostInput(values));
        reset(roleFormValuesFrom(created));
        toast.success(`“${created.title}” created as a draft`);
        router.replace(`/admin/roles/${created.id}`);
      }
    } catch (error) {
      applyServerErrors(error, setError, roleFormFields);
    }
  }

  // useWatch subscribes this component to one field. watch() returns a new
  // function each render, which the React Compiler cannot memoise safely.
  const titleLength = useWatch({ control, name: "title" }).length;
  const errorCount = Object.keys(errors).filter((k) => k !== "root").length;
  const cancelHref = editing ? `/admin/roles/${post.id}` : "/admin/roles";

  // Each input's aria-invalid and aria-describedby tie it to its error, so a
  // screen reader reads the problem when the field gets focus.
  const describe = (name: keyof RoleFormValues, hint?: boolean) =>
    [hint ? `${name}-hint` : null, errors[name] ? `${name}-error` : null].filter(Boolean).join(" ") || undefined;

  return (
    <form onSubmit={handleSubmit(onSubmit)} noValidate className="flex max-w-3xl flex-col gap-8">
      {errors.root?.server && (
        <div role="alert" className="flex items-start gap-2 rounded-lg border border-destructive/40 bg-surface p-3 text-sm">
          <AlertTriangleIcon className="mt-0.5 size-4 shrink-0 text-destructive" aria-hidden="true" />
          <p>{errors.root.server.message}</p>
        </div>
      )}

      <FieldSet>
        <FieldLegend>The role</FieldLegend>
        <FieldGroup>
          <Field data-invalid={!!errors.title}>
            <div className="flex items-baseline justify-between gap-2">
              <FieldLabel htmlFor="title">Title</FieldLabel>
              <span
                id="title-hint"
                className={`tabular text-xs ${titleLength > MAX_TITLE_LENGTH ? "text-destructive" : "text-muted-foreground"}`}
              >
                {titleLength}/{MAX_TITLE_LENGTH}
              </span>
            </div>
            <Input
              id="title"
              autoFocus={!editing}
              aria-invalid={!!errors.title}
              aria-describedby={describe("title", true)}
              {...register("title")}
            />
            <FieldError id="title-error" errors={[errors.title]} />
          </Field>

          <Field data-invalid={!!errors.description}>
            <FieldLabel htmlFor="description">Description</FieldLabel>
            <Textarea
              id="description"
              rows={6}
              aria-describedby={describe("description", true)}
              {...register("description")}
            />
            <FieldDescription id="description-hint">
              What the job is and who it suits. Candidates read this before they apply.
            </FieldDescription>
          </Field>
        </FieldGroup>
      </FieldSet>

      <FieldSet>
        <FieldLegend>Where and when</FieldLegend>
        <div className="grid gap-5 sm:grid-cols-3">
          <Field>
            <FieldLabel htmlFor="employment_type">Employment type</FieldLabel>
            <NativeSelect id="employment_type" {...register("employment_type")}>
              <option value="">Not specified</option>
              {employmentTypes.map((t) => (
                <option key={t.value} value={t.value}>
                  {t.label}
                </option>
              ))}
              {/* A stored value outside the list (set through the API) stays
                  selectable, so opening the form does not silently change it. */}
              {post?.employment_type && !employmentTypes.some((t) => t.value === post.employment_type) && (
                <option value={post.employment_type}>{post.employment_type}</option>
              )}
            </NativeSelect>
          </Field>
          <Field>
            <FieldLabel htmlFor="location">Location</FieldLabel>
            <Input id="location" placeholder="Remote, Dhaka…" {...register("location")} />
          </Field>
          <Field data-invalid={!!errors.closes_at}>
            <FieldLabel htmlFor="closes_at">Closes on</FieldLabel>
            <Input
              id="closes_at"
              type="date"
              aria-invalid={!!errors.closes_at}
              aria-describedby={describe("closes_at", editing)}
              {...register("closes_at")}
            />
            {editing && post.closes_at && (
              <FieldDescription id="closes_at-hint">Can be moved, not removed.</FieldDescription>
            )}
            <FieldError id="closes_at-error" errors={[errors.closes_at]} />
          </Field>
        </div>
      </FieldSet>

      <FieldSet>
        <FieldLegend>Openings and pay</FieldLegend>
        <div className="grid gap-5 sm:grid-cols-3">
          <Field data-invalid={!!errors.vacancies}>
            <FieldLabel htmlFor="vacancies">Vacancies</FieldLabel>
            <Input
              id="vacancies"
              inputMode="numeric"
              className="tabular"
              aria-invalid={!!errors.vacancies}
              aria-describedby={describe("vacancies")}
              {...register("vacancies")}
            />
            <FieldError id="vacancies-error" errors={[errors.vacancies]} />
          </Field>
          <Field data-invalid={!!errors.salary_min}>
            <FieldLabel htmlFor="salary_min">Salary from</FieldLabel>
            <Input
              id="salary_min"
              inputMode="decimal"
              className="tabular"
              aria-invalid={!!errors.salary_min}
              aria-describedby={describe("salary_min", editing)}
              // deps: re-validate salary_max as well when this changes. The
              // min-versus-max error lives on salary_min, so editing the
              // maximum must be able to clear it, and vice versa below.
              {...register("salary_min", { deps: ["salary_max"] })}
            />
            <FieldError id="salary_min-error" errors={[errors.salary_min]} />
          </Field>
          <Field data-invalid={!!errors.salary_max}>
            <FieldLabel htmlFor="salary_max">Salary to</FieldLabel>
            <Input
              id="salary_max"
              inputMode="decimal"
              className="tabular"
              aria-invalid={!!errors.salary_max}
              aria-describedby={describe("salary_max")}
              {...register("salary_max", { deps: ["salary_min"] })}
            />
            <FieldError id="salary_max-error" errors={[errors.salary_max]} />
          </Field>
        </div>
        {editing && (post.salary_min !== null || post.salary_max !== null) && (
          <FieldDescription id="salary_min-hint">
            A salary that is already set can be changed but not removed: the API keeps the stored amount when a field
            is left blank.
          </FieldDescription>
        )}
      </FieldSet>

      <div className="flex flex-col gap-3 border-t pt-5 sm:flex-row sm:items-center">
        <div className="flex gap-2">
          <Button type="submit" size="lg" disabled={!isValid || isSubmitting || (editing && !isDirty)}>
            {isSubmitting ? "Saving…" : editing ? "Save changes" : "Create draft"}
          </Button>
          <Link href={cancelHref} className={buttonVariants({ variant: "ghost", size: "lg" })}>
            Cancel
          </Link>
        </div>
        {/* The reason the button is disabled, stated rather than implied. */}
        <p className="text-sm text-muted-foreground" aria-live="polite">
          {errorCount > 0
            ? `Fix ${errorCount === 1 ? "the highlighted field" : `the ${errorCount} highlighted fields`} to continue.`
            : !isValid
              ? "A title and at least one vacancy are required."
              : editing && !isDirty
                ? "No changes yet."
                : null}
        </p>
      </div>
    </form>
  );
}
