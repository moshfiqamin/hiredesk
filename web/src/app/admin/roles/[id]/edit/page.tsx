"use client";

import { useParams } from "next/navigation";

import { QueryView } from "@/components/states/states";
import { RoleForm } from "@/components/roles/role-form";
import { Skeleton } from "@/components/ui/skeleton";
import { useJobPost } from "@/hooks/use-job-posts";

export default function EditRolePage() {
  const { id } = useParams<{ id: string }>();
  const post = useJobPost(id);

  return (
    <QueryView
      query={post}
      errorTitle="Could not load this role"
      loading={
        <div className="flex max-w-3xl flex-col gap-4" aria-busy="true" aria-label="Loading the form">
          <Skeleton className="h-8" />
          <Skeleton className="h-32" />
          <Skeleton className="h-8 w-2/3" />
        </div>
      }
    >
      {/* The key remounts the form for a different post, so its defaults are
          never left over from the last one. */}
      {(data) => <RoleForm key={data.id} post={data} />}
    </QueryView>
  );
}
