import { PlusIcon } from "lucide-react";
import type { Metadata } from "next";
import Link from "next/link";

import { PageHeader } from "@/components/layout/page-header";
import { RolesView } from "@/components/roles/roles-view";
import { buttonVariants } from "@/components/ui/button";

export const metadata: Metadata = { title: "Roles" };

export default function RolesPage() {
  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Roles"
        description="Every job you are hiring for, from first draft to closed."
        actions={
          // The one primary button on the page: the filled accent is reserved
          // for it, so the eye finds it without reading.
          <Link href="/admin/roles/new" className={buttonVariants({ size: "lg" })}>
            <PlusIcon aria-hidden="true" />
            Create role
          </Link>
        }
      />
      <RolesView />
    </div>
  );
}
