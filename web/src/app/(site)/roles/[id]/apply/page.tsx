import type { Metadata } from "next";
import { Suspense } from "react";

import { RoleGate } from "@/components/auth/role-gate";
import { ApplicationForm } from "@/components/candidate/application-form";
import { Skeleton } from "@/components/ui/skeleton";

export const metadata: Metadata = { title: "Apply" };

export default function ApplyPage() {
  return (
    // RoleGate renders in the browser only (the session is in localStorage),
    // so the server sends this fallback.
    <Suspense fallback={<Skeleton className="h-96" />}>
      <RoleGate role="candidate">
        <ApplicationForm />
      </RoleGate>
    </Suspense>
  );
}
