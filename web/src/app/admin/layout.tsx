import { Suspense } from "react";

import { AdminShell } from "@/components/layout/admin-shell";

export default function AdminLayout({ children }: LayoutProps<"/admin">) {
  return (
    // The fallback is what the server sends, since AdminShell renders only
    // in the browser. Kept as an empty page-coloured frame so nothing jumps
    // when the shell appears.
    <Suspense fallback={<div className="min-h-dvh bg-background" aria-busy="true" />}>
      <AdminShell>{children}</AdminShell>
    </Suspense>
  );
}
