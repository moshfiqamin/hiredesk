import type { Metadata } from "next";
import { Suspense } from "react";

import { ApplicationsView } from "@/components/applications/applications-view";
import { PageHeader } from "@/components/layout/page-header";
import { Skeleton } from "@/components/ui/skeleton";

export const metadata: Metadata = { title: "Applications" };

export default function ApplicationsPage() {
  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Applications"
        description="Every application across your roles. Sort by score to read it as a shortlist."
      />
      {/* useSearchParams reads the URL, which is not known while prerendering;
          the boundary keeps that from holding back the page around it. */}
      <Suspense fallback={<Skeleton className="h-96" />}>
        <ApplicationsView />
      </Suspense>
    </div>
  );
}
