import type { Metadata } from "next";
import { Suspense } from "react";

import { RoleGate } from "@/components/auth/role-gate";
import { InterviewerApplication } from "@/components/interviewer/interviewer-application";
import { Skeleton } from "@/components/ui/skeleton";

export const metadata: Metadata = { title: "Review" };

export default function AssignmentPage() {
  return (
    <Suspense fallback={<Skeleton className="h-96" />}>
      <RoleGate role="interviewer">
        <InterviewerApplication />
      </RoleGate>
    </Suspense>
  );
}
