"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { ArrowRight } from "lucide-react";
import { useSearchParams } from "next/navigation";
import { useState } from "react";
import { useForm } from "react-hook-form";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useLanguage } from "@/lib/i18n";
import { type LoginInput, loginSchema } from "@/lib/schemas";

export function LoginForm() {
  const { t } = useLanguage();
  const searchParams = useSearchParams();
  const nextPath = searchParams.get("next") ?? "/skus";
  const [error, setError] = useState<string | null>(null);
  const form = useForm<LoginInput>({
    resolver: zodResolver(loginSchema),
    defaultValues: {
      email: "",
      password: "",
    },
  });

  async function onSubmit(values: LoginInput) {
    setError(null);
    const response = await fetch("/api/auth/login", {
      method: "POST",
      headers: {
        "content-type": "application/json",
      },
      body: JSON.stringify(values),
    });

    if (!response.ok) {
      setError(t("loginError"));
      return;
    }

    // A full navigation ensures the httpOnly session cookie written by the
    // login route is visible to the middleware before it evaluates the
    // protected destination. This matters behind HTTPS reverse proxies such
    // as a Cloudflare development tunnel.
    window.location.assign(nextPath);
  }

  return (
    <div>
      <form action={`/api/auth/login?next=${encodeURIComponent(nextPath)}`} className="space-y-4" method="post" onSubmit={form.handleSubmit(onSubmit)}>
        <div className="space-y-2">
          <Label htmlFor="email">{t("email")}</Label>
          <Input id="email" type="email" autoComplete="email" {...form.register("email")} />
          {form.formState.errors.email ? (
            <p className="text-xs font-medium text-danger" role="alert">{t("emailInvalid")}</p>
          ) : null}
        </div>
        <div className="space-y-2">
          <Label htmlFor="password">{t("password")}</Label>
          <Input id="password" type="password" autoComplete="current-password" {...form.register("password")} />
          {form.formState.errors.password ? (
            <p className="text-xs font-medium text-danger" role="alert">{t("passwordInvalid")}</p>
          ) : null}
        </div>
        {error ? <p className="rounded-lg border border-danger/25 bg-danger/5 p-3 text-sm font-medium text-danger" role="alert">{error}</p> : null}
        <Button className="mt-2 w-full" disabled={form.formState.isSubmitting} type="submit">
          {form.formState.isSubmitting ? t("loggingIn") : t("login")}
          <ArrowRight className="h-4 w-4" />
        </Button>
      </form>
    </div>
  );
}
