import { SiteShell } from "@/components/layout/site-shell";

// Every page outside /admin and the sign-in screens: the public role pages,
// the candidate's applications and the interviewer's assignments.
export default function SiteLayout({ children }: LayoutProps<"/">) {
  return <SiteShell>{children}</SiteShell>;
}
