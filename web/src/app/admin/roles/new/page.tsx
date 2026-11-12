import type { Metadata } from "next";
import Link from "next/link";

import { PageHeader } from "@/components/layout/page-header";
import { RoleForm } from "@/components/roles/role-form";

export const metadata: Metadata = { title: "Create role" };

export default function NewRolePage() {
  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        eyebrow={
          <Link href="/admin/roles" className="hover:underline">
            Roles
          </Link>
        }
        title="Create role"
        description="A new role starts as a private draft. Nobody can apply until you open it."
      />
      <RoleForm />
    </div>
  );
}
