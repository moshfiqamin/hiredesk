"use client";

import { BriefcaseIcon } from "lucide-react";

import { PageHeader } from "@/components/layout/page-header";
import { EmptyState, QueryView } from "@/components/states/states";
import { Skeleton } from "@/components/ui/skeleton";
import { useJobPosts } from "@/hooks/use-job-posts";

// A first, plain use of useJobPosts and QueryView, the pattern every page
// follows. The full roles table replaces this list in module 11.3.
export default function RolesPage() {
  const jobPosts = useJobPosts();

  return (
    <div className="flex flex-col gap-6">
      <PageHeader title="Roles" description="Every job post, drafts included." />
      <QueryView
        query={jobPosts}
        errorTitle="Could not load roles"
        loading={
          <div className="flex flex-col gap-2" aria-busy="true" aria-label="Loading roles">
            {Array.from({ length: 4 }, (_, i) => (
              <Skeleton key={i} className="h-10" />
            ))}
          </div>
        }
        isEmpty={(posts) => posts.length === 0}
        empty={<EmptyState icon={BriefcaseIcon} title="No roles yet" />}
      >
        {(posts) => (
          <ul className="divide-y rounded-lg border bg-surface">
            {posts.map((post) => (
              <li key={post.id} className="flex items-center justify-between px-4 py-2.5">
                <span className="font-medium">{post.title}</span>
                <span className="text-sm text-muted-foreground">{post.status}</span>
              </li>
            ))}
          </ul>
        )}
      </QueryView>
    </div>
  );
}
