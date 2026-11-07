"use client";

import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { Suspense, useState } from "react";
import { Loader2Icon } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { FormError } from "@/components/form-error";
import { ApiError } from "@/lib/api";
import { useLogin, useRegister } from "@/lib/queries";
import { safeNext } from "@/lib/format";

function RegisterForm() {
  const router = useRouter();
  const params = useSearchParams();
  const register = useRegister();
  const login = useLogin();
  // Stays true through the redirect after success.
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<ApiError | null>(null);
  const [fallbackError, setFallbackError] = useState<string | null>(null);

  async function onSubmit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault();
    const form = new FormData(e.currentTarget);
    const email = String(form.get("email"));
    const password = String(form.get("password"));
    setPending(true);
    setError(null);
    setFallbackError(null);
    try {
      await register.mutateAsync({ name: String(form.get("name")), email, password });
      // Register does not start a session; sign in right away.
      await login.mutateAsync({ email, password });
      router.replace(safeNext(params.get("next")));
      router.refresh();
    } catch (err) {
      if (err instanceof ApiError) setError(err);
      else setFallbackError("Something went wrong. Try again.");
      setPending(false);
    }
  }

  const fieldErr = (f: string) => error?.fieldError(f);
  const topError = error && error.fields.length === 0 ? error.message : fallbackError;

  return (
    <form onSubmit={onSubmit} className="flex flex-col gap-6" noValidate>
      <div>
        <h1 className="text-3xl font-semibold tracking-tight">Create your account</h1>
        <p className="mt-1 text-sm text-muted-foreground">Start a workspace and invite your team.</p>
      </div>
      <FormError message={topError} />
      <FieldGroup>
        <Field data-invalid={!!fieldErr("name") || undefined}>
          <FieldLabel htmlFor="name">Full name</FieldLabel>
          <Input id="name" name="name" autoComplete="name" placeholder="Ada Lovelace" aria-invalid={!!fieldErr("name")} required />
          <FieldError>{fieldErr("name")}</FieldError>
        </Field>
        <Field data-invalid={!!fieldErr("email") || undefined}>
          <FieldLabel htmlFor="email">Work email</FieldLabel>
          <Input id="email" name="email" type="email" autoComplete="email" placeholder="you@company.com" aria-invalid={!!fieldErr("email")} required />
          <FieldError>{fieldErr("email")}</FieldError>
        </Field>
        <Field data-invalid={!!fieldErr("password") || undefined}>
          <FieldLabel htmlFor="password">Password</FieldLabel>
          <Input
            id="password"
            name="password"
            type="password"
            autoComplete="new-password"
            minLength={12}
            maxLength={72}
            aria-invalid={!!fieldErr("password")}
            required
          />
          {fieldErr("password") ? (
            <FieldError>{fieldErr("password")}</FieldError>
          ) : (
            <FieldDescription>12–72 characters.</FieldDescription>
          )}
        </Field>
      </FieldGroup>
      <Button type="submit" size="lg" disabled={pending}>
        {pending && <Loader2Icon className="animate-spin" />}
        Create account
      </Button>
      <p className="text-center text-sm text-muted-foreground">
        Already have an account?{" "}
        <Link
          href={{ pathname: "/login", query: params.get("next") ? { next: params.get("next") } : {} }}
          className="font-medium text-foreground underline-offset-4 hover:underline"
        >
          Sign in
        </Link>
      </p>
    </form>
  );
}

export default function RegisterPage() {
  return (
    <Suspense>
      <RegisterForm />
    </Suspense>
  );
}
