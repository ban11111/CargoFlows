"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Check,
  CheckCircle2,
  CircleDashed,
  Eye,
  EyeOff,
  KeyRound,
  LoaderCircle,
  LockKeyhole,
  RotateCw,
  ShieldAlert,
  TriangleAlert,
} from "lucide-react";
import { FormEvent, useEffect, useRef, useState } from "react";

import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { openAIKeySchema } from "@/lib/ai-schemas";
import { ApiError, apiRequest } from "@/lib/api";
import { useLanguage, type MessageKey } from "@/lib/i18n";
import type { components } from "@/lib/openapi-types";

type OpenAISetting = components["schemas"]["OpenAISetting"];

const emptySetting: OpenAISetting = {
  provider: "openai",
  status: "unconfigured",
  key_fingerprint: "",
  verified_at: null,
  image_capability_verified_at: null,
  last_used_at: null,
};

async function getSetting() {
  try {
    return await safeOpenAIRequest<OpenAISetting>("/settings/openai");
  } catch (error) {
    if (error instanceof ApiError && error.status === 404) return emptySetting;
    throw error;
  }
}

async function safeOpenAIRequest<TResponse>(path: string, options?: RequestInit) {
  try {
    return await apiRequest<TResponse>(path, options);
  } catch (error) {
    if (error instanceof ApiError) {
      throw new ApiError("OpenAI settings request failed", error.status);
    }
    throw new Error("OpenAI settings request failed");
  }
}

export default function OpenAISettingsPage() {
  const { language, t } = useLanguage();
  const queryClient = useQueryClient();
  const [secret, setSecretState] = useState("");
  const secretRef = useRef("");
  const [showSecret, setShowSecret] = useState(false);
  const [fieldError, setFieldError] = useState<MessageKey | null>(null);
  const [notice, setNotice] = useState<MessageKey | null>(null);

  function setSecret(value: string) {
    secretRef.current = value;
    setSecretState(value);
  }

  function clearSecret() {
    secretRef.current = "";
    setSecretState("");
    setShowSecret(false);
  }

  useEffect(() => () => {
    secretRef.current = "";
  }, []);

  const settingQuery = useQuery({
    queryKey: ["openai-setting"],
    queryFn: getSetting,
    retry: false,
  });
  const setting = settingQuery.data ?? emptySetting;
  const forbidden = settingQuery.error instanceof ApiError && settingQuery.error.status === 403;

  const saveMutation = useMutation({
    mutationFn: () => safeOpenAIRequest<OpenAISetting>("/settings/openai", {
      method: "PUT",
      body: JSON.stringify({ api_key: secretRef.current }),
    }),
    onSuccess(next) {
      queryClient.setQueryData(["openai-setting"], next);
      clearSecret();
      setFieldError(null);
      setNotice("openAISaveSuccess");
    },
  });

  const disableMutation = useMutation({
    mutationFn: () => safeOpenAIRequest<OpenAISetting>("/settings/openai", { method: "DELETE" }),
    onSuccess(next) {
      queryClient.setQueryData(["openai-setting"], next);
      clearSecret();
      setNotice("openAIDisableSuccess");
    },
  });

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setNotice(null);
    setFieldError(null);
    saveMutation.reset();
    const parsed = openAIKeySchema.safeParse({ api_key: secret });
    if (!parsed.success) {
      setFieldError(parsed.error.issues[0]?.message === "openAIKeyTooLong" ? "openAIKeyTooLong" : "openAIKeyTooShort");
      return;
    }
    if (setting.key_fingerprint && !window.confirm(t("openAIReplaceConfirm"))) return;
    secretRef.current = parsed.data.api_key;
    saveMutation.mutate();
  }

  function disable() {
    setNotice(null);
    disableMutation.reset();
    if (!window.confirm(t("openAIDisableConfirm"))) return;
    disableMutation.mutate();
  }

  if (settingQuery.isLoading) {
    return <p className="flex min-h-44 items-center justify-center gap-2 text-sm text-muted-foreground" role="status"><LoaderCircle className="h-4 w-4 animate-spin motion-reduce:animate-none" />{t("openAILoading")}</p>;
  }

  if (forbidden) {
    return <div className="mx-auto max-w-2xl rounded-lg border border-warning/30 bg-card p-6" role="alert"><ShieldAlert className="mb-3 h-6 w-6 text-warning" /><h1 className="text-xl font-semibold">{t("openAISettingsTitle")}</h1><p className="mt-2 text-sm leading-6 text-muted-foreground">{t("openAIForbidden")}</p></div>;
  }

  if (settingQuery.isError) {
    return <div className="mx-auto max-w-2xl rounded-lg border border-danger/30 bg-danger/5 p-5" role="alert"><div className="flex items-start gap-3"><TriangleAlert className="mt-0.5 h-5 w-5 shrink-0 text-danger" /><div><h1 className="font-semibold text-danger">{t("openAILoadError")}</h1><Button aria-label={t("openAIRetryLoad")} className="mt-4 min-h-11" onClick={() => settingQuery.refetch()} variant="secondary"><RotateCw className="h-4 w-4" />{t("openAIRetryLoad")}</Button></div></div></div>;
  }

  const configured = Boolean(setting.key_fingerprint);
  const verified = configured && Boolean(setting.verified_at) && setting.status === "active";
  const imageReady = verified && Boolean(setting.image_capability_verified_at);
  const statusLabel = setting.status === "active" ? t("openAIActive") : setting.status === "invalid" ? t("openAIInvalid") : setting.status === "disabled" ? t("openAIDisabled") : t("openAIUnconfigured");

  return (
    <div className="mx-auto max-w-6xl space-y-6">
      <header className="border-b border-border pb-5">
        <p className="mb-2 flex items-center gap-2 text-xs font-semibold uppercase tracking-[0.14em] text-primary"><LockKeyhole className="h-4 w-4" />CargoFlow · OpenAI</p>
        <h1 className="text-2xl font-semibold tracking-tight">{t("openAISettingsTitle")}</h1>
        <p className="mt-1 max-w-2xl text-sm leading-6 text-muted-foreground">{t("openAISettingsDescription")}</p>
      </header>

      <div className="grid gap-6 lg:grid-cols-[minmax(0,0.85fr)_minmax(0,1.15fr)] lg:items-start">
        <Card className="overflow-hidden">
          <CardHeader><CardTitle className="flex items-center gap-2"><KeyRound className="h-4 w-4 text-primary" />{t("openAICredentialStatus")}</CardTitle></CardHeader>
          <CardContent className="space-y-5">
            <p className="text-lg font-semibold" role="status">{statusLabel}</p>
            <ol className="relative space-y-0" aria-label={t("openAICredentialStatus")}>
              <StatusNode complete={configured} label={t("openAIConfiguredNode")} ready={t("openAINodeReady")} pending={t("openAINodePending")} />
              <StatusNode complete={verified} label={t("openAIVerifiedNode")} ready={t("openAINodeReady")} pending={t("openAINodePending")} />
              <StatusNode complete={imageReady} last label={t("openAIImageReadyNode")} ready={t("openAINodeReady")} pending={t("openAINodePending")} />
            </ol>
            <dl className="grid gap-3 border-t border-border pt-4 text-sm sm:grid-cols-2 lg:grid-cols-1 xl:grid-cols-2">
              <Detail label={t("openAIProvider")} value="OpenAI" />
              <Detail label={t("openAICompatibility")} value={t("openAICompatibilityValue")} />
              <Detail label={t("openAIKeyFingerprint")} value={setting.key_fingerprint || t("openAINever")} mono />
              <Detail label={t("openAIVerifiedAt")} value={formatDate(setting.verified_at, language, t("openAINever"))} />
              <Detail label={t("openAIImageVerifiedAt")} value={formatDate(setting.image_capability_verified_at, language, t("openAINever"))} />
              <Detail label={t("openAILastUsedAt")} value={formatDate(setting.last_used_at, language, t("openAINever"))} />
            </dl>
          </CardContent>
        </Card>

        <div className="space-y-8">
          <Card>
            <CardHeader><CardTitle>{t("openAICredentialForm")}</CardTitle></CardHeader>
            <CardContent>
              <form className="space-y-5" onSubmit={submit}>
                <div className="space-y-2">
                  <Label htmlFor="openai-api-key">{t("openAIKeyLabel")}</Label>
                  <div className="relative">
                    <Input aria-describedby="openai-key-help openai-key-error" aria-invalid={Boolean(fieldError)} autoComplete="new-password" className="h-11 pr-14" id="openai-api-key" maxLength={512} onChange={(event) => setSecret(event.target.value)} placeholder={t("openAIKeyPlaceholder")} type={showSecret ? "text" : "password"} value={secret} />
                    <Button aria-label={showSecret ? t("openAIHideKey") : t("openAIShowKey")} className="absolute right-0 top-0 min-h-11 min-w-11" onClick={() => setShowSecret((current) => !current)} size="icon" type="button" variant="ghost">{showSecret ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}</Button>
                  </div>
                  <p className="text-sm leading-5 text-muted-foreground" id="openai-key-help">{t("openAIKeyHelper")}</p>
                  {fieldError ? <p className="text-sm text-danger" id="openai-key-error" role="alert">{t(fieldError)}</p> : null}
                </div>
                {saveMutation.isError ? <p className="rounded-md border border-danger/30 bg-danger/5 p-3 text-sm text-danger" role="alert">{saveMutation.error instanceof ApiError && saveMutation.error.status === 422 ? t("openAIRejectedError") : t("openAISaveError")}</p> : null}
                <div className="flex justify-end">
                  <Button className="min-h-11" disabled={saveMutation.isPending || disableMutation.isPending} type="submit">{saveMutation.isPending ? <LoaderCircle className="h-4 w-4 animate-spin motion-reduce:animate-none" /> : <Check className="h-4 w-4" />}{saveMutation.isPending ? t("openAISaving") : configured ? t("openAIReplaceVerify") : t("openAISaveVerify")}</Button>
                </div>
              </form>
            </CardContent>
          </Card>

          {configured && setting.status !== "disabled" ? <section className="rounded-lg border border-danger/30 bg-card p-5" aria-labelledby="openai-danger-title"><div className="flex items-start gap-3"><ShieldAlert className="mt-0.5 h-5 w-5 shrink-0 text-danger" /><div className="min-w-0 flex-1"><h2 className="font-semibold text-danger" id="openai-danger-title">{t("openAIDangerZone")}</h2><p className="mt-1 text-sm leading-6 text-muted-foreground">{t("openAIDangerDescription")}</p><Button className="mt-4 min-h-11" disabled={disableMutation.isPending || saveMutation.isPending} onClick={disable} variant="danger">{disableMutation.isPending ? <LoaderCircle className="h-4 w-4 animate-spin motion-reduce:animate-none" /> : <ShieldAlert className="h-4 w-4" />}{disableMutation.isPending ? t("openAIDisabling") : t("openAIDisable")}</Button>{disableMutation.isError ? <p className="mt-3 text-sm text-danger" role="alert">{t("openAIDisableError")}</p> : null}</div></div></section> : null}
        </div>
      </div>
      {notice ? <div aria-live="polite" className="flex items-center gap-2 rounded-md border border-success/30 bg-success/5 p-3 text-sm text-success" role="status"><CheckCircle2 className="h-4 w-4" />{t(notice)}</div> : null}
    </div>
  );
}

function StatusNode({ complete, label, ready, pending, last = false }: { complete: boolean; label: string; ready: string; pending: string; last?: boolean }) {
  return <li className="relative flex min-h-16 gap-3"><span className={`relative z-10 grid h-8 w-8 shrink-0 place-items-center rounded-full border bg-card ${complete ? "border-primary text-primary" : "border-border text-muted-foreground"}`}>{complete ? <CheckCircle2 className="h-4 w-4" /> : <CircleDashed className="h-4 w-4" />}</span>{!last ? <span aria-hidden="true" className={`absolute left-[15px] top-8 h-8 w-px ${complete ? "bg-primary" : "bg-border"}`} /> : null}<div className="pt-1"><p className="text-sm font-medium">{label}</p><p className="mt-0.5 text-xs text-muted-foreground">{complete ? ready : pending}</p></div></li>;
}

function Detail({ label, value, mono = false }: { label: string; value: string; mono?: boolean }) {
  return <div><dt className="text-xs text-muted-foreground">{label}</dt><dd className={`mt-1 break-words ${mono ? "font-mono tabular-nums" : ""}`}>{value}</dd></div>;
}

function formatDate(value: string | null, language: "zh" | "en", empty: string) {
  if (!value) return empty;
  return new Intl.DateTimeFormat(language === "zh" ? "zh-CN" : "en", { dateStyle: "medium", timeStyle: "short" }).format(new Date(value));
}
