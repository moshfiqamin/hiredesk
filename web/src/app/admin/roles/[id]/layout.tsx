import { RoleShell } from "@/components/roles/role-shell";

export default function RoleLayout({ children }: LayoutProps<"/admin/roles/[id]">) {
  return <RoleShell>{children}</RoleShell>;
}
