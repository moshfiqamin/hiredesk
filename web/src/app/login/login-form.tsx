"use client";

import { useMutation } from "@tanstack/react-query";
import { useRouter } from "next/navigation";
import { useState, type FormEvent } from "react";

import { Button } from "@/components/ui/button";
import { Field, FieldError, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { api } from "@/lib/api/client";
import { errorMessage } from "@/lib/api/errors";
import type { TokenPair } from "@/lib/api/types";
import { homeFor, safeNext } from "@/lib/auth/home";
import { sessionFromTokens, setSession } from "@/lib/auth/session";

/** LoginForm signs in with email and password. */
export function LoginForm() {
  const router = useRouter();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");

  const login = useMutation({
    mutationFn: () =>
      api<TokenPair>("/v1/auth/login", { method: "POST", body: { email, password }, auth: false }),
    onSuccess: (tokens) => {
      setSession(sessionFromTokens(tokens));
      // Back to where they were going, or to the page their role works from.
      router.replace(safeNext() ?? homeFor(tokens.user.role));
    },
  });

  function onSubmit(event: FormEvent) {
    // Stops the browser's own form submission, which would reload the page.
    event.preventDefault();
    login.mutate();
  }

  return (
    <form onSubmit={onSubmit} noValidate>
      <FieldGroup>
        <Field>
          <FieldLabel htmlFor="email">Email</FieldLabel>
          <Input
            id="email"
            type="email"
            autoComplete="username"
            required
            value={email}
            onChange={(e) => setEmail(e.target.value)}
          />
        </Field>
        <Field>
          <FieldLabel htmlFor="password">Password</FieldLabel>
          <Input
            id="password"
            type="password"
            autoComplete="current-password"
            required
            value={password}
            onChange={(e) => setPassword(e.target.value)}
          />
        </Field>
        {login.isError && <FieldError>{errorMessage(login.error)}</FieldError>}
        <Button type="submit" size="lg" disabled={login.isPending || !email || !password}>
          {login.isPending ? "Signing in…" : "Sign in"}
        </Button>
      </FieldGroup>
    </form>
  );
}
