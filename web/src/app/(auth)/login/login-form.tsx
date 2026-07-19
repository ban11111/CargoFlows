"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { ArrowRight, LockKeyhole } from "lucide-react";
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
    window.location.assign(searchParams.get("next") ?? "/skus");
  }

  return (
    <div>
      <div className="mb-4 flex h-10 w-10 items-center justify-center rounded-md bg-primary text-primary-foreground">
        <LockKeyhole className="h-5 w-5" />
      </div>
      <form className="space-y-4" onSubmit={form.handleSubmit(onSubmit)}>
        <div className="space-y-2">
          <Label htmlFor="email">{t("email")}</Label>
          <Input id="email" type="email" autoComplete="email" {...form.register("email")} />
          {form.formState.errors.email ? (
            <p className="text-xs text-danger">{t("emailInvalid")}</p>
          ) : null}
        </div>
        <div className="space-y-2">
          <Label htmlFor="password">{t("password")}</Label>
          <Input id="password" type="password" autoComplete="current-password" {...form.register("password")} />
          {form.formState.errors.password ? (
            <p className="text-xs text-danger">{t("passwordInvalid")}</p>
          ) : null}
        </div>
        {error ? <p className="text-sm text-danger">{error}</p> : null}
        <Button className="w-full" disabled={form.formState.isSubmitting} type="submit">
          {form.formState.isSubmitting ? t("loggingIn") : t("login")}
          <ArrowRight className="h-4 w-4" />
        </Button>
      </form>
    </div>
  );
}
