"use client";

import { useMutation } from "@tanstack/react-query";
import { useRouter } from "next/navigation";
import { useState, type FormEvent } from "react";

import { Button } from "@/components/ui/button";
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { api } from "@/lib/api/client";
import { ApiError, errorMessage, fieldErrors } from "@/lib/api/errors";
import type { TokenPair } from "@/lib/api/types";
import { homeFor, safeNext } from "@/lib/auth/home";
import { sessionFromTokens, setSession } from "@/lib/auth/session";

/**
 * RegisterForm creates a candidate account and signs straight in. The API
 * only ever registers candidates: staff accounts are made by the seed or an
 * admin, never by a public form.
 */
export function RegisterForm() {
  const router = useRouter();
  const [fullName, setFullName] = useState("");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");

  const register = useMutation({
    mutationFn: async () => {
      await api("/v1/auth/register", { method: "POST", auth: false, body: { full_name: fullName, email, password } });
      // Registration returns the user, not tokens, so sign in with the same
      // credentials rather than make the person type them again.
      return api<TokenPair>("/v1/auth/login", { method: "POST", auth: false, body: { email, password } });
    },
    onSuccess: (tokens) => {
      setSession(sessionFromTokens(tokens));
      router.replace(safeNext() ?? homeFor(tokens.user.role));
    },
  });

  const errors = fieldErrors(register.error);
  const taken = register.error instanceof ApiError && register.error.status === 409;
  // A problem no field explains (network, a taken email) is shown once, below.
  const general = register.isError && Object.keys(errors).length === 0 ? errorMessage(register.error) : null;

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    register.mutate();
  }

  return (
    <form onSubmit={onSubmit} noValidate>
      <FieldGroup>
        <Field data-invalid={Boolean(errors.full_name) || undefined}>
          <FieldLabel htmlFor="full_name">Full name</FieldLabel>
          <Input
            id="full_name"
            autoComplete="name"
            required
            value={fullName}
            onChange={(e) => setFullName(e.target.value)}
            aria-invalid={Boolean(errors.full_name) || undefined}
            aria-describedby={errors.full_name ? "full_name-error" : undefined}
          />
          {errors.full_name && <FieldError id="full_name-error">Full name {errors.full_name}</FieldError>}
        </Field>
        <Field data-invalid={Boolean(errors.email || taken) || undefined}>
          <FieldLabel htmlFor="email">Email</FieldLabel>
          <Input
            id="email"
            type="email"
            autoComplete="email"
            required
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            aria-invalid={Boolean(errors.email || taken) || undefined}
            aria-describedby={errors.email ? "email-error" : undefined}
          />
          {errors.email && <FieldError id="email-error">Email {errors.email}</FieldError>}
        </Field>
        <Field data-invalid={Boolean(errors.password) || undefined}>
          <FieldLabel htmlFor="password">Password</FieldLabel>
          <Input
            id="password"
            type="password"
            autoComplete="new-password"
            required
            minLength={8}
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            aria-invalid={Boolean(errors.password) || undefined}
            aria-describedby={errors.password ? "password-hint password-error" : "password-hint"}
          />
          <FieldDescription id="password-hint">At least 8 characters.</FieldDescription>
          {errors.password && <FieldError id="password-error">Password {errors.password}</FieldError>}
        </Field>
        {general && <FieldError role="alert">{general}</FieldError>}
        <Button type="submit" size="lg" disabled={register.isPending || !fullName || !email || password.length < 8}>
          {register.isPending ? "Creating your account…" : "Create account"}
        </Button>
      </FieldGroup>
    </form>
  );
}
