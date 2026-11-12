"use client";

import { UserPlusIcon } from "lucide-react";
import { useState } from "react";

import { ErrorState } from "@/components/states/states";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle, DialogTrigger } from "@/components/ui/dialog";
import { Skeleton } from "@/components/ui/skeleton";
import { useAssignment, useInterviewers } from "@/hooks/use-reviews";
import type { Assignment, InterviewerLoad } from "@/lib/api/types";

/**
 * AssignDialog lets an admin choose who interviews a candidate. The list is
 * GET /v1/admin/interviewers, which returns interviewers only, so a
 * candidate or another admin can never be offered (and the API would refuse
 * one with a 422 regardless). Each person shows their current load, and the
 * list is ordered lightest load first, so the default choice spreads the
 * work instead of piling it on whoever is at the top of the alphabet.
 */
export function AssignDialog({
  applicationId,
  candidateName,
  assignments,
}: {
  applicationId: string;
  candidateName: string;
  assignments: Assignment[];
}) {
  const [open, setOpen] = useState(false);
  const interviewers = useInterviewers();
  const change = useAssignment(applicationId);
  const assigned = new Map(assignments.map((a) => [a.interviewer_id, a]));

  const rows = [...(interviewers.data ?? [])].sort((a, b) => {
    // Already assigned first, so revoking is one glance away; then by load.
    const aa = assigned.has(a.id) ? 0 : 1;
    const ab = assigned.has(b.id) ? 0 : 1;
    return aa - ab || a.open_assignments - b.open_assignments || a.full_name.localeCompare(b.full_name);
  });
  // The heaviest load sets the scale of the load bars, so they compare people
  // with each other rather than against an arbitrary number.
  const maxLoad = Math.max(1, ...rows.map((r) => r.open_assignments));
  const mean = rows.length > 0 ? rows.reduce((s, r) => s + r.open_assignments, 0) / rows.length : 0;

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger render={<Button variant="outline" size="sm" />}>
        <UserPlusIcon aria-hidden="true" />
        Manage
      </DialogTrigger>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>Interviewers for {candidateName}</DialogTitle>
          <DialogDescription>
            Load counts open assignments (candidates not yet at offer or rejected). Lightest load is listed first.
          </DialogDescription>
        </DialogHeader>

        {interviewers.isError ? (
          <ErrorState error={interviewers.error} title="Could not load the interviewers" onRetry={() => void interviewers.refetch()} />
        ) : !interviewers.data ? (
          <div className="flex flex-col gap-2" aria-busy="true" aria-label="Loading interviewers">
            <Skeleton className="h-14" />
            <Skeleton className="h-14" />
          </div>
        ) : rows.length === 0 ? (
          <p className="text-sm text-text-secondary">There are no interviewer accounts yet.</p>
        ) : (
          <ul className="flex flex-col divide-y rounded-lg border">
            {rows.map((person) => (
              <InterviewerRow
                key={person.id}
                person={person}
                assignment={assigned.get(person.id)}
                maxLoad={maxLoad}
                heavy={person.open_assignments > mean + 2}
                busy={change.isPending && change.variables?.interviewerId === person.id}
                onAssign={() => change.mutate({ interviewerId: person.id, name: person.full_name, assign: true })}
                onRemove={() => change.mutate({ interviewerId: person.id, name: person.full_name, assign: false })}
              />
            ))}
          </ul>
        )}
      </DialogContent>
    </Dialog>
  );
}

function InterviewerRow({
  person,
  assignment,
  maxLoad,
  heavy,
  busy,
  onAssign,
  onRemove,
}: {
  person: InterviewerLoad;
  assignment: Assignment | undefined;
  maxLoad: number;
  heavy: boolean;
  busy: boolean;
  onAssign: () => void;
  onRemove: () => void;
}) {
  const load = `${person.open_assignments} open, ${person.pending_reviews} awaiting review`;
  return (
    <li className="flex items-center gap-3 px-3 py-3">
      <div className="flex min-w-0 flex-1 flex-col gap-1">
        <p className="flex flex-wrap items-center gap-x-2 text-sm font-medium">
          <span className="truncate">{person.full_name}</span>
          {assignment && (
            <span className="text-xs font-normal text-primary-ink">
              Assigned{assignment.reviewed ? " · reviewed" : ""}
            </span>
          )}
        </p>
        <div className="flex items-center gap-2">
          {/* The bar repeats the numbers beside it; it is decorative. */}
          <span aria-hidden="true" className="h-1.5 w-16 overflow-hidden rounded-full bg-surface-sunken">
            <span
              className={heavy ? "block h-full bg-band-adequate" : "block h-full bg-primary"}
              style={{ width: `${(person.open_assignments / maxLoad) * 100}%` }}
            />
          </span>
          <span className="text-xs text-text-secondary">
            {load}
            {heavy && <span className="font-medium text-band-adequate"> · heavier than most</span>}
          </span>
        </div>
      </div>
      {assignment ? (
        <Button variant="outline" size="sm" onClick={onRemove} disabled={busy} aria-label={`Remove ${person.full_name}`}>
          Remove
        </Button>
      ) : (
        <Button size="sm" onClick={onAssign} disabled={busy} aria-label={`Assign ${person.full_name}`}>
          Assign
        </Button>
      )}
    </li>
  );
}
