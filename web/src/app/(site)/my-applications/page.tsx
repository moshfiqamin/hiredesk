import type { Metadata } from "next";
import { Suspense } from "react";

import { RoleGate } from "@/components/auth/role-gate";
import { MyApplications } from "@/components/candidate/my-applications";
import { Skeleton } from "@/components/ui/skeleton";

export const metadata: Metadata = { title: "My applications" };

export default function MyApplicationsPage() {
  return (
    <Suspense fallback={<Skeleton className="h-64" />}>
      <RoleGate role="candidate">
        <MyApplications />
      </RoleGate>
    </Suspense>
  );
}
