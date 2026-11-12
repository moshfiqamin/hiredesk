"use client";

import { ArrowLeftIcon, BriefcaseIcon, CalendarIcon, MapPinIcon, UsersIcon, WalletIcon } from "lucide-react";
import Link from "next/link";
import { useParams } from "next/navigation";

import { ErrorState } from "@/components/states/states";
import { buttonVariants } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { useHydrated } from "@/hooks/use-hydrated";
import { useListing } from "@/hooks/use-listings";
import { ApiError } from "@/lib/api/errors";
import type { JobListingDetail } from "@/lib/api/types";
import { useSession } from "@/lib/auth/session";
import { employmentTypeLabel, formatDate, placesLeft, salaryRange } from "@/lib/format";
import { describeQuestion } from "@/lib/questions/describe";

/** RoleDetail is an open role's public page: everything a candidate should know before signing up. */
export function RoleDetail() {
  const { id } = useParams<{ id: string }>();
  const listing = useListing(id);

  if (listing.isError) {
    const gone = listing.error instanceof ApiError && listing.error.status === 404;
    return gone ? (
      <div className="flex max-w-prose flex-col gap-3 py-8">
        <h1 className="text-2xl font-semibold tracking-tight">This role is not open</h1>
        <p className="text-text-secondary">It may have closed, or the link may be wrong.</p>
        <Link href="/roles" className={buttonVariants({ variant: "outline" }) + " w-fit"}>
          See the open roles
        </Link>
      </div>
    ) : (
      <ErrorState error={listing.error} title="Could not load this role" onRetry={() => void listing.refetch()} />
    );
  }
  if (!listing.data) {
    return (
      <div className="flex flex-col gap-4" aria-busy="true" aria-label="Loading the role">
        <Skeleton className="h-10 w-2/3" />
        <Skeleton className="h-40" />
        <Skeleton className="h-64" />
      </div>
    );
  }
  return <RoleBody role={listing.data} />;
}

function RoleBody({ role }: { role: JobListingDetail }) {
  const pay = salaryRange(role.salary_min, role.salary_max);
  const facts = [
    role.location && { icon: MapPinIcon, label: "Location", value: role.location },
    role.employment_type && { icon: BriefcaseIcon, label: "Type", value: employmentTypeLabel(role.employment_type) },
    pay && { icon: WalletIcon, label: "Salary", value: pay },
    { icon: UsersIcon, label: "Places", value: placesLeft(role.remaining_vacancies) },
    role.closes_at && { icon: CalendarIcon, label: "Closes", value: formatDate(role.closes_at) },
  ].filter((f): f is { icon: typeof MapPinIcon; label: string; value: string } => Boolean(f));

  return (
    <div className="grid gap-10 lg:grid-cols-[minmax(0,1fr)_20rem]">
      {/* React 19 hoists a <title> rendered anywhere into <head>: the tab
          names the role, which the static metadata cannot know. */}
      <title>{`${role.title} · HireDesk`}</title>
      <article className="flex min-w-0 flex-col gap-8">
        <header className="flex flex-col gap-4">
          <Link href="/roles" className="inline-flex w-fit items-center gap-1 rounded-sm text-sm text-muted-foreground outline-none hover:underline focus-visible:ring-2 focus-visible:ring-ring">
            <ArrowLeftIcon className="size-3.5" aria-hidden="true" />
            All open roles
          </Link>
          <h1 className="text-3xl font-semibold tracking-tight text-balance">{role.title}</h1>
          <dl className="grid grid-cols-2 gap-x-6 gap-y-3 sm:flex sm:flex-wrap">
            {/* Each pair in its own <div>, the one wrapper a <dl> allows;
                the icon sits inside the <dt>, not between the pair. */}
            {facts.map(({ icon: Icon, label, value }) => (
              <div key={label} className="grid grid-cols-[auto_1fr] gap-x-2">
                <dt className="contents text-xs text-muted-foreground">
                  <Icon className="row-span-2 mt-0.5 size-4" aria-hidden="true" />
                  <span>{label}</span>
                </dt>
                <dd className="col-start-2 text-sm font-medium">{value}</dd>
              </div>
            ))}
          </dl>
          {role.skill_tags.length > 0 && (
            <ul aria-label="Skills this role looks for" className="flex flex-wrap gap-1.5">
              {role.skill_tags.map((tag) => (
                <li key={tag} className="rounded-full bg-surface-sunken px-2.5 py-0.5 font-mono text-xs text-text-secondary">
                  {tag}
                </li>
              ))}
            </ul>
          )}
        </header>

        {role.description && (
          <section aria-labelledby="about-heading" className="flex flex-col gap-3">
            <h2 id="about-heading" className="text-lg font-semibold">
              About the role
            </h2>
            {/* Plain text from the admin form; pre-line keeps their paragraphs. */}
            <p className="max-w-prose text-md leading-relaxed whitespace-pre-line text-text-secondary">{role.description}</p>
          </section>
        )}

        <section aria-labelledby="questions-heading" className="flex flex-col gap-3">
          <h2 id="questions-heading" className="text-lg font-semibold">
            What you will be asked
          </h2>
          <p className="max-w-prose text-text-secondary">
            The application is these {role.questions.length} questions and a link to your CV. Your draft is saved in this
            browser as you type.
          </p>
          <ol className="flex flex-col divide-y rounded-lg border bg-surface">
            {role.questions.map((q, i) => (
              <li key={q.id} className="flex gap-3 px-4 py-3">
                <span className="font-mono text-sm text-muted-foreground tabular-nums">{i + 1}.</span>
                <div className="flex min-w-0 flex-col gap-0.5">
                  <span className="font-medium break-words">{q.prompt}</span>
                  <span className="text-sm text-text-secondary">
                    {describeQuestion(q)} · {q.required ? "Required" : "Optional"}
                  </span>
                </div>
              </li>
            ))}
          </ol>
        </section>
      </article>

      <aside className="lg:sticky lg:top-20 lg:self-start">
        <ApplyPanel role={role} />
      </aside>
    </div>
  );
}

/** ApplyPanel offers the next step for whoever is looking: apply, sign up, or nothing to do. */
function ApplyPanel({ role }: { role: JobListingDetail }) {
  const hydrated = useHydrated();
  const session = useSession();
  const applyPath = `/roles/${role.id}/apply`;

  return (
    <div className="flex flex-col gap-3 rounded-xl border bg-surface p-5">
      <h2 className="font-semibold">Interested?</h2>
      {!hydrated ? (
        <Skeleton className="h-9" />
      ) : session?.user.role === "candidate" ? (
        <Link href={applyPath} className={buttonVariants({ size: "lg" })}>
          Apply for this role
        </Link>
      ) : session ? (
        <p className="text-sm text-text-secondary">
          You are signed in as {session.user.role === "admin" ? "an admin" : "an interviewer"}. Applications are made
          from a candidate account.
        </p>
      ) : (
        <>
          <Link href={`/register?next=${encodeURIComponent(applyPath)}`} className={buttonVariants({ size: "lg" })}>
            Create an account to apply
          </Link>
          <p className="text-sm text-text-secondary">
            Already have one?{" "}
            <Link href={`/login?next=${encodeURIComponent(applyPath)}`} className="font-medium text-foreground underline-offset-2 hover:underline">
              Sign in
            </Link>
          </p>
        </>
      )}
    </div>
  );
}
