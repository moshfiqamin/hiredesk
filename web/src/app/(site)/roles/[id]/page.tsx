import { Suspense } from "react";

import { RoleDetail } from "@/components/listings/role-detail";
import { Skeleton } from "@/components/ui/skeleton";

export default function RoleDetailPage() {
  return (
    // useParams is request-time data in this Next.js version, so the page
    // streams in behind a placeholder shaped like it.
    <Suspense fallback={<Skeleton className="h-96" aria-label="Loading the role" />}>
      <RoleDetail />
    </Suspense>
  );
}
