import type { Metadata } from "next";
import { Suspense } from "react";

import { RoleGate } from "@/components/auth/role-gate";
import { AssignmentsView } from "@/components/interviewer/assignments-view";
import { Skeleton } from "@/components/ui/skeleton";

export const metadata: Metadata = { title: "My assignments" };

export default function AssignmentsPage() {
  return (
    <Suspense fallback={<Skeleton className="h-64" />}>
      <RoleGate role="interviewer">
        <AssignmentsView />
      </RoleGate>
    </Suspense>
  );
}
