import type { Metadata } from "next";
import Link from "next/link";

import { ThemeToggle } from "@/components/theme-toggle";

import { RegisterForm } from "./register-form";

export const metadata: Metadata = { title: "Create an account" };

export default function RegisterPage() {
  return (
    <main id="main" className="flex min-h-dvh flex-col items-center justify-center gap-6 px-4 py-12">
      <div className="w-full max-w-sm rounded-lg border bg-surface p-6">
        <div className="mb-6 flex items-start justify-between gap-4">
          <div>
            <Link href="/roles" className="text-sm font-medium text-primary-ink hover:underline">
              HireDesk
            </Link>
            <h1 className="mt-1 text-xl font-semibold tracking-tight">Create an account</h1>
          </div>
          <ThemeToggle />
        </div>
        <RegisterForm />
        <p className="mt-6 text-sm text-text-secondary">
          Already have an account?{" "}
          <Link href="/login" className="font-medium text-foreground underline-offset-2 hover:underline">
            Sign in
          </Link>
        </p>
      </div>
    </main>
  );
}
