import type { Metadata } from "next";

import { ThemeToggle } from "@/components/theme-toggle";

import { LoginForm } from "./login-form";

export const metadata: Metadata = { title: "Sign in" };

export default function LoginPage() {
  return (
    <main className="flex min-h-dvh flex-col items-center justify-center gap-6 px-4 py-12">
      <div className="w-full max-w-sm rounded-lg border bg-surface p-6">
        <div className="mb-6 flex items-start justify-between gap-4">
          <div>
            <p className="text-sm font-medium text-primary-ink">HireDesk</p>
            <h1 className="mt-1 text-xl font-semibold tracking-tight">Sign in</h1>
          </div>
          <ThemeToggle />
        </div>
        <LoginForm />
      </div>
      <p className="max-w-sm text-center text-sm text-muted-foreground">
        Running locally? <code className="font-mono">make seed</code> creates an admin account; its
        email and password are in the README.
      </p>
    </main>
  );
}
