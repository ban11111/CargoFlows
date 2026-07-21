"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Check,
  CheckCircle2,
  CircleDashed,
  Eye,
  EyeOff,
  Gauge,
  KeyRound,
  LoaderCircle,
  LockKeyhole,
  MonitorCog,
  RotateCw,
  ShieldAlert,
  TriangleAlert,
} from "lucide-react";
import { FormEvent, useEffect, useRef, useState } from "react";

import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { ErrorNotice } from "@/components/error-notice";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { OpenAICostSettings } from "@/components/openai-cost-settings";
import { openAIKeySchema } from "@/lib/ai-schemas";
import { ApiError, apiRequest } from "@/lib/api";
import { useLanguage, type MessageKey } from "@/lib/i18n";
import type { components } from "@/lib/openapi-types";

type OpenAISetting = components["schemas"]["OpenAISetting"];
type OpenAIModel = components["schemas"]["OpenAIModel"];

const emptySetting: OpenAISetting = {
  provider: "openai",
  status: "unconfigured",
  key_fingerprint: "",
  text_model: "gpt-5.6-terra",
  image_model: "gpt-5.6",
  image_api_mode: "responses",
  image_responses_model: "gpt-5.6",
  image_generation_model: "gpt-image-2",
  verified_at: null,
  image_capability_verified_at: null,
  image_responses_verified_at: null,
  image_generation_verified_at: null,
  last_used_at: null,
  max_workers_per_job: 3,
  max_workers_global: 9,
};

async function getSetting() {
  try {
    return normalizeSetting(await safeOpenAIRequest<OpenAISetting>("/settings/openai"));
  } catch (error) {
    if (error instanceof ApiError && error.status === 404) return emptySetting;
    throw error;
  }
}

function normalizeSetting(value: OpenAISetting): OpenAISetting {
  const legacyImage = value.image_model || emptySetting.image_model;
  const legacyDirect = legacyImage.toLowerCase().startsWith("gpt-image-");
  return {
    ...emptySetting,
    ...value,
    image_api_mode: value.image_api_mode ?? (legacyDirect ? "images" : "responses"),
    image_responses_model: value.image_responses_model ?? (legacyDirect ? emptySetting.image_responses_model : legacyImage),
    image_generation_model: value.image_generation_model ?? (legacyDirect ? legacyImage : emptySetting.image_generation_model),
    max_workers_per_job: Number.isInteger(value.max_workers_per_job) ? value.max_workers_per_job : emptySetting.max_workers_per_job,
    max_workers_global: Number.isInteger(value.max_workers_global) ? value.max_workers_global : emptySetting.max_workers_global,
  };
}

async function getModels() {
  const response = await safeOpenAIRequest<{ data: OpenAIModel[] }>("/settings/openai/models");
  return Array.isArray(response.data) ? response.data.map(normalizeModel) : [];
}

function normalizeModel(model: OpenAIModel): OpenAIModel {
  if (typeof model.supports_text === "boolean") return model;
  const directImage = model.id.toLowerCase().startsWith("gpt-image-");
  return { ...model, supports_text: !directImage, supports_image_tool: !directImage, supports_images_api: directImage };
}

async function safeOpenAIRequest<TResponse>(path: string, options?: RequestInit) {
  try {
    return await apiRequest<TResponse>(path, options);
  } catch (error) {
    if (error instanceof ApiError) {
      throw new ApiError(error.message, error.status, error.code, error.requestId, error.details);
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
  const configured = Boolean(setting.key_fingerprint);

  const saveMutation = useMutation({
    mutationFn: () => safeOpenAIRequest<OpenAISetting>("/settings/openai", {
      method: "PUT",
      body: JSON.stringify({ api_key: secretRef.current }),
    }),
    onSuccess(next) {
      queryClient.setQueryData(["openai-setting"], next);
      queryClient.invalidateQueries({ queryKey: ["openai-models"] });
      clearSecret();
      setFieldError(null);
      setNotice("openAISaveSuccess");
    },
  });

  const disableMutation = useMutation({
    mutationFn: () => safeOpenAIRequest<OpenAISetting>("/settings/openai", { method: "DELETE" }),
    onSuccess(next) {
      queryClient.setQueryData(["openai-setting"], next);
      queryClient.removeQueries({ queryKey: ["openai-models"] });
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

  const verified = configured && Boolean(setting.verified_at) && setting.status === "active";
  const imageReady = verified && Boolean(setting.image_api_mode === "images" ? setting.image_generation_verified_at : setting.image_responses_verified_at);
  const statusLabel = setting.status === "active" ? t("openAIActive") : setting.status === "invalid" ? t("openAIInvalid") : setting.status === "disabled" ? t("openAIDisabled") : t("openAIUnconfigured");

  return (
    <div className="mx-auto max-w-6xl space-y-6">
      <header className="border-b border-border pb-6">
        <p className="mb-2 flex items-center gap-2 text-xs font-semibold uppercase tracking-[0.14em] text-primary"><LockKeyhole className="h-4 w-4" />CargoFlows · OpenAI</p>
        <h1 className="text-3xl font-bold tracking-tight text-navy sm:text-4xl">{t("openAISettingsTitle")}</h1>
        <p className="mt-2 max-w-2xl text-sm leading-6 text-muted-foreground">{t("openAISettingsDescription")}</p>
      </header>

	  <OpenAICostSettings />

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
              <Detail label={t("openAITextModelLabel")} value={setting.text_model} mono />
              <Detail label={language === "zh" ? "图片调用方式" : "Image API mode"} value={setting.image_api_mode === "images" ? "Images API" : "Responses"} />
              <Detail label={language === "zh" ? "Responses 编排模型" : "Responses orchestration model"} value={setting.image_responses_model} mono />
              <Detail label={language === "zh" ? "Images API 图像模型" : "Images API image model"} value={setting.image_generation_model} mono />
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

          <WorkerSettingsCard setting={setting} />
          {verified ? <ModelSettingsCard key={setting.key_fingerprint} setting={setting} /> : null}
          {configured && setting.status !== "disabled" ? <section className="rounded-lg border border-danger/30 bg-card p-5" aria-labelledby="openai-danger-title"><div className="flex items-start gap-3"><ShieldAlert className="mt-0.5 h-5 w-5 shrink-0 text-danger" /><div className="min-w-0 flex-1"><h2 className="font-semibold text-danger" id="openai-danger-title">{t("openAIDangerZone")}</h2><p className="mt-1 text-sm leading-6 text-muted-foreground">{t("openAIDangerDescription")}</p><Button className="mt-4 min-h-11" disabled={disableMutation.isPending || saveMutation.isPending} onClick={disable} variant="danger">{disableMutation.isPending ? <LoaderCircle className="h-4 w-4 animate-spin motion-reduce:animate-none" /> : <ShieldAlert className="h-4 w-4" />}{disableMutation.isPending ? t("openAIDisabling") : t("openAIDisable")}</Button>{disableMutation.isError ? <p className="mt-3 text-sm text-danger" role="alert">{t("openAIDisableError")}</p> : null}</div></div></section> : null}
        </div>
      </div>
      {notice ? (
        <div aria-live="polite" className="flex items-center gap-2 rounded-md border border-success/30 bg-success/5 p-3 text-sm text-success" role="status">
          <CheckCircle2 className="h-4 w-4" />{t(notice)}
        </div>
      ) : null}
    </div>
  );
}

function WorkerSettingsCard({ setting }: { setting: OpenAISetting }) {
  const { t } = useLanguage();
  const queryClient = useQueryClient();
  const [perJob, setPerJob] = useState(String(setting.max_workers_per_job));
  const [global, setGlobal] = useState(String(setting.max_workers_global));
  const [perJobError, setPerJobError] = useState<MessageKey | null>(null);
  const [globalError, setGlobalError] = useState<MessageKey | null>(null);
  const [saved, setSaved] = useState(false);
  const changed = perJob !== String(setting.max_workers_per_job) || global !== String(setting.max_workers_global);

  function validate() {
    const nextPerJob = parseWorkerLimit(perJob);
    const nextGlobal = parseWorkerLimit(global);
    const nextPerJobError: MessageKey | null = nextPerJob === null
      ? "openAIWorkersRangeError"
      : nextGlobal !== null && nextPerJob > nextGlobal
        ? "openAIWorkersRelationError"
        : null;
    const nextGlobalError: MessageKey | null = nextGlobal === null ? "openAIWorkersRangeError" : null;
    setPerJobError(nextPerJobError);
    setGlobalError(nextGlobalError);
    return nextPerJobError === null && nextGlobalError === null && nextPerJob !== null && nextGlobal !== null
      ? { max_workers_per_job: nextPerJob, max_workers_global: nextGlobal }
      : null;
  }

  const saveWorkers = useMutation({
    mutationFn: (body: { max_workers_per_job: number; max_workers_global: number }) => safeOpenAIRequest<OpenAISetting>("/settings/openai/workers", {
      method: "PATCH",
      body: JSON.stringify(body),
    }),
    onSuccess(next) {
      queryClient.setQueryData(["openai-setting"], normalizeSetting(next));
      setSaved(true);
    },
  });

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setSaved(false);
    saveWorkers.reset();
    const value = validate();
    if (value) saveWorkers.mutate(value);
  }

  return <Card>
    <CardHeader><CardTitle className="flex items-center gap-2"><Gauge className="h-4 w-4 text-primary" />{t("openAIWorkersTitle")}</CardTitle></CardHeader>
    <CardContent>
      <form className="space-y-5" onSubmit={submit}>
        <p className="text-sm leading-6 text-muted-foreground">{t("openAIWorkersIntro")}</p>
        <div className="grid gap-5 sm:grid-cols-2">
          <div className="space-y-2">
            <Label htmlFor="openai-workers-per-job">{t("openAIWorkersPerJobLabel")}</Label>
            <Input aria-describedby="openai-workers-per-job-help openai-workers-per-job-error" aria-invalid={Boolean(perJobError)} className="h-11 tabular-nums" id="openai-workers-per-job" inputMode="numeric" max={32} min={1} onBlur={validate} onChange={(event) => { setPerJob(event.target.value); setPerJobError(null); setSaved(false); }} step={1} type="number" value={perJob} />
            <p className="text-sm leading-5 text-muted-foreground" id="openai-workers-per-job-help">{t("openAIWorkersPerJobHelp")}</p>
            {perJobError ? <p className="text-sm text-danger" id="openai-workers-per-job-error" role="alert">{t(perJobError)}</p> : null}
          </div>
          <div className="space-y-2">
            <Label htmlFor="openai-workers-global">{t("openAIWorkersGlobalLabel")}</Label>
            <Input aria-describedby="openai-workers-global-help openai-workers-global-error" aria-invalid={Boolean(globalError)} className="h-11 tabular-nums" id="openai-workers-global" inputMode="numeric" max={32} min={1} onBlur={validate} onChange={(event) => { setGlobal(event.target.value); setGlobalError(null); setSaved(false); }} step={1} type="number" value={global} />
            <p className="text-sm leading-5 text-muted-foreground" id="openai-workers-global-help">{t("openAIWorkersGlobalHelp")}</p>
            {globalError ? <p className="text-sm text-danger" id="openai-workers-global-error" role="alert">{t(globalError)}</p> : null}
          </div>
        </div>
        {saveWorkers.isError ? <p className="rounded-md border border-danger/30 bg-danger/5 p-3 text-sm text-danger" role="alert">{t("openAIWorkersSaveError")}</p> : null}
        {saved ? <p className="flex items-center gap-2 text-sm text-success" role="status"><CheckCircle2 className="h-4 w-4" />{t("openAIWorkersSaveSuccess")}</p> : null}
        <div className="flex justify-end border-t border-border pt-4">
          <Button className="min-h-11" disabled={!changed || saveWorkers.isPending} type="submit">{saveWorkers.isPending ? <LoaderCircle className="h-4 w-4 animate-spin motion-reduce:animate-none" /> : <Check className="h-4 w-4" />}{saveWorkers.isPending ? t("openAIWorkersSaving") : t("openAIWorkersSave")}</Button>
        </div>
      </form>
    </CardContent>
  </Card>;
}

function parseWorkerLimit(value: string) {
  if (!/^\d+$/.test(value)) return null;
  const parsed = Number(value);
  return Number.isInteger(parsed) && parsed >= 1 && parsed <= 32 ? parsed : null;
}

function ModelSettingsCard({ setting }: { setting: OpenAISetting }) {
  const { language, t } = useLanguage();
  const zh = language === "zh";
  const queryClient = useQueryClient();
  const [textModel, setTextModel] = useState(setting.text_model);
  const [imageAPIMode, setImageAPIMode] = useState<"responses" | "images">(setting.image_api_mode);
  const [imageResponsesModel, setImageResponsesModel] = useState(setting.image_responses_model);
  const [imageGenerationModel, setImageGenerationModel] = useState(setting.image_generation_model);
  const [saved, setSaved] = useState(false);
  const modelsQuery = useQuery({
    queryKey: ["openai-models", setting.key_fingerprint],
    queryFn: getModels,
    retry: false,
    staleTime: 0,
  });
  const models = modelsQuery.data ?? [];
  const changed = textModel !== setting.text_model || imageAPIMode !== setting.image_api_mode || imageResponsesModel !== setting.image_responses_model || imageGenerationModel !== setting.image_generation_model;
  const canSave = modelsQuery.isSuccess && models.length > 0 && changed;
  const saveModels = useMutation({
    mutationFn: () => safeOpenAIRequest<OpenAISetting>("/settings/openai/models", {
      method: "PATCH",
      body: JSON.stringify({
        text_model: textModel,
        image_api_mode: imageAPIMode,
        image_responses_model: imageResponsesModel,
        image_generation_model: imageGenerationModel,
      }),
    }),
    onSuccess(next) {
      queryClient.setQueryData(["openai-setting"], next);
      setSaved(true);
    },
  });
  const options = (current: string, predicate: (model: OpenAIModel) => boolean) => {
    const compatible = models.filter(predicate);
    return compatible.some((model) => model.id === current)
      ? compatible
      : [{ id: current, owned_by: "", supports_text: false, supports_image_tool: false, supports_images_api: false, compatibility_reason: zh ? "当前配置；该模型未出现在兼容列表中" : "Current setting; not present in the compatible list" }, ...compatible];
  };
  const placeholder = modelsQuery.isLoading
    ? t("openAIModelsLoading")
    : modelsQuery.isError
      ? t("openAIModelsUnavailable")
      : t("openAIModelsEmpty");
  const disabled = modelsQuery.isLoading || modelsQuery.isError || models.length === 0 || saveModels.isPending;

  return <Card>
    <CardHeader><CardTitle className="flex items-center gap-2"><MonitorCog className="h-4 w-4 text-primary" />{t("openAIModelsTitle")}</CardTitle></CardHeader>
    <CardContent className="space-y-5">
      <p className="text-sm leading-6 text-muted-foreground">{t("openAIModelsIntro")}</p>
      <ModelSelect id="openai-text-model" label={t("openAITextModelLabel")} help={t("openAITextModelHelper")} value={textModel} onChange={(value) => { setTextModel(value); setSaved(false); saveModels.reset(); }} models={options(textModel, (model) => model.supports_text)} placeholder={placeholder} disabled={disabled} />

      <fieldset className="space-y-3 rounded-lg border border-border p-4">
        <legend className="px-1 text-sm font-semibold">{zh ? "图片调用方式" : "Image API mode"}</legend>
        {(["responses", "images"] as const).map((mode) => <label className={`flex min-h-11 cursor-pointer items-start gap-3 rounded-md border p-3 ${imageAPIMode === mode ? "border-primary bg-primary/5" : "border-border"}`} key={mode}><input checked={imageAPIMode === mode} className="mt-1" name="image-api-mode" onChange={() => { setImageAPIMode(mode); setSaved(false); saveModels.reset(); }} type="radio" value={mode} /><span><span className="block text-sm font-medium">{mode === "responses" ? (zh ? "Responses 对话编排" : "Responses orchestration") : (zh ? "Images API 直接生成/编辑" : "Direct Images API")}</span><span className="mt-1 block text-xs leading-5 text-muted-foreground">{mode === "responses" ? (zh ? "使用主模型规划图片工具调用，适合多轮和上下文编排。" : "A mainline model orchestrates the image tool for contextual, multi-turn work.") : (zh ? "直接调用图像生成或编辑接口，gpt-image-2 应选择此方式。" : "Calls image generation or editing directly; use this mode for gpt-image-2.")}</span></span></label>)}
      </fieldset>

      <ModelSelect id="openai-image-responses-model" label={t("openAIImageModelLabel")} help={zh ? "Responses 编排模型：必须支持 image_generation 工具；不能选择 gpt-image-*。" : "Responses orchestration model: must support the image_generation tool; gpt-image-* is not valid here."} value={imageResponsesModel} onChange={(value) => { setImageResponsesModel(value); setSaved(false); saveModels.reset(); }} models={options(imageResponsesModel, (model) => model.supports_image_tool)} placeholder={placeholder} disabled={disabled} />
      <ModelSelect id="openai-image-generation-model" label={zh ? "Images API 图像模型" : "Images API image model"} help={zh ? "用于 /v1/images/generations 和 /v1/images/edits，例如 gpt-image-2。" : "Used by /v1/images/generations and /v1/images/edits, such as gpt-image-2."} value={imageGenerationModel} onChange={(value) => { setImageGenerationModel(value); setSaved(false); saveModels.reset(); }} models={options(imageGenerationModel, (model) => model.supports_images_api)} placeholder={placeholder} disabled={disabled} />
      {modelsQuery.isError ? <ErrorNotice actionLabel={t("openAIModelsRefresh")} message={zh ? "后端无法使用当前凭据读取 OpenAI 模型列表。请检查密钥、组织权限和网络后重试。" : "The backend could not read the OpenAI model list with the current credential. Check the key, organization access, and network, then retry."} onAction={() => modelsQuery.refetch()} requestId={modelsQuery.error instanceof ApiError ? modelsQuery.error.requestId : ""} title={zh ? "无法获取模型列表" : "Could not load models"} /> : null}
      {saveModels.isError ? <ErrorNotice message={saveModels.error instanceof ApiError ? saveModels.error.message : t("openAIModelsSaveError")} recovery={zh ? "确认每个模型与所选 API 路径兼容，然后重新保存。" : "Confirm each model is compatible with its selected API path, then save again."} requestId={saveModels.error instanceof ApiError ? saveModels.error.requestId : ""} title={zh ? "模型配置未保存" : "Model settings were not saved"} /> : null}
      {saved ? <p className="flex items-center gap-2 text-sm text-success" role="status"><CheckCircle2 className="h-4 w-4" />{t("openAIModelsSaveSuccess")}</p> : null}
      <div className="flex flex-wrap items-center justify-between gap-3 border-t border-border pt-4">
        <p aria-live="polite" className="text-xs text-muted-foreground">{modelsQuery.isSuccess ? t("openAIModelsCount").replace("{count}", String(models.length)) : ""}</p>
        <div className="flex gap-2">
          <Button className="min-h-11" disabled={modelsQuery.isFetching || saveModels.isPending} onClick={() => { setSaved(false); modelsQuery.refetch(); }} type="button" variant="secondary"><RotateCw className={`h-4 w-4 ${modelsQuery.isFetching ? "animate-spin motion-reduce:animate-none" : ""}`} />{t("openAIModelsRefresh")}</Button>
          <Button className="min-h-11" disabled={!canSave || saveModels.isPending} onClick={() => saveModels.mutate()} type="button">{saveModels.isPending ? <LoaderCircle className="h-4 w-4 animate-spin motion-reduce:animate-none" /> : <Check className="h-4 w-4" />}{saveModels.isPending ? t("openAIModelsSaving") : t("openAIModelsSave")}</Button>
        </div>
      </div>
    </CardContent>
  </Card>;
}

function ModelSelect({ id, label, help, value, onChange, models, placeholder, disabled }: { id: string; label: string; help: string; value: string; onChange: (value: string) => void; models: OpenAIModel[]; placeholder: string; disabled: boolean }) {
  return <div className="space-y-2">
    <Label htmlFor={id}>{label}</Label>
    <select aria-describedby={`${id}-help`} className="flex min-h-11 w-full rounded-md border border-input bg-background px-3 py-2 font-mono text-sm shadow-sm outline-none transition-colors focus-visible:border-ring focus-visible:ring-2 focus-visible:ring-ring/30 disabled:cursor-not-allowed disabled:opacity-50" disabled={disabled} id={id} onChange={(event) => onChange(event.target.value)} value={value}>
      {models.length === 0 ? <option value="">{placeholder}</option> : null}
      {models.map((model) => <option key={model.id} value={model.id}>{model.id}{model.owned_by ? ` · ${model.owned_by}` : ""}</option>)}
    </select>
    <p className="text-sm leading-5 text-muted-foreground" id={`${id}-help`}>{help}</p>
  </div>;
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
