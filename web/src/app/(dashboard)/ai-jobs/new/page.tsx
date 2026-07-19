"use client";

import { useMutation, useQuery } from "@tanstack/react-query";
import { AlertCircle, ArrowLeft, ArrowRight, Check, FileCheck2, Image as ImageIcon, Languages, Settings2, ShieldCheck } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useMemo, useRef, useState } from "react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { apiRequest } from "@/lib/api";
import { type MessageKey, useLanguage } from "@/lib/i18n";
import type { components } from "@/lib/openapi-types";

type Template = components["schemas"]["AIContentTemplate"];
type Slot = components["schemas"]["AIContentSlot"];
type Job = components["schemas"]["AIJob"];
type Override = components["schemas"]["AIJobGenerationOverride"];
type SKU = { public_id: string; code: string; status: string; product: { name: string } };
type AssetCategory = components["schemas"]["AssetReviewCategory"];

const selectClass = "min-h-11 w-full rounded-md border border-border bg-card px-3 text-sm outline-none transition-colors focus:border-primary focus:ring-2 focus:ring-primary/20";

function list<T>(value: unknown): T[] {
  return Array.isArray(value) ? value as T[] : [];
}

function initialOverride(slot: Slot): Override {
  const config = slot.generation_config as Record<string, unknown>;
  const candidates = list<number>(config.allowed_candidate_count);
  const sizes = list<Override["size"]>(config.allowed_sizes);
  const qualities = list<Override["quality"]>(config.allowed_qualities);
  const styles = list<string>(config.allowed_styles);
  return {
    ...(candidates.length ? { candidate_count: candidates[0] } : {}),
    ...(slot.kind === "image" && sizes.length ? { size: sizes[0] } : {}),
    ...(slot.kind === "image" && qualities.length ? { quality: qualities[0] } : {}),
    ...(slot.kind === "image" && styles.length ? { style: styles[0] } : {}),
  };
}

function slotKind(kind: Slot["kind"], zh: boolean) {
  if (kind === "image") return zh ? "图片" : "Image";
  if (kind === "title") return zh ? "商品标题" : "Product title";
  return zh ? "搜索优化描述" : "Search description";
}

export default function NewAIJobPage() {
  const router = useRouter();
  const { language, t } = useLanguage();
  const zh = language === "zh";
  const idempotencyKey = useRef(`ai-job-${crypto.randomUUID()}`);
  const [step, setStep] = useState(1);
  const [skuID, setSkuID] = useState("");
  const [versionID, setVersionID] = useState("");
  const [locale, setLocale] = useState("zh-CN");
  const [selectedSlots, setSelectedSlots] = useState<string[]>([]);
  const [selectedAssets, setSelectedAssets] = useState<string[]>([]);
  const [overrides, setOverrides] = useState<Record<string, Override>>({});
  const [preference, setPreference] = useState("");
  const [error, setError] = useState<MessageKey | null>(null);

  const skusQuery = useQuery({ queryKey: ["skus", "ai-job-options"], queryFn: () => apiRequest<{ data: SKU[] }>("/skus") });
  const templatesQuery = useQuery({ queryKey: ["ai-content-templates"], queryFn: () => apiRequest<{ data: Template[] }>("/ai-content-templates") });
  const assetsQuery = useQuery({ queryKey: ["assets", "review", "hierarchy", "approved"], queryFn: () => apiRequest<{ data: AssetCategory[] }>("/assets/review/hierarchy?status=approved") });

  const versions = useMemo(() => (templatesQuery.data?.data ?? []).flatMap((template) => template.versions.filter((version) => version.status === "published").map((version) => ({ template, version }))), [templatesQuery.data]);
  const selection = versions.find(({ version }) => version.public_id === versionID);
  const slots = useMemo(() => [...(selection?.version.slots ?? [])].sort((a, b) => a.sequence - b.sequence), [selection]);
  const chosenSlots = slots.filter((slot) => selectedSlots.includes(slot.slot_key));
  const approvedForSKU = useMemo(() => (assetsQuery.data?.data ?? []).flatMap((category) => category.skus).find((sku) => sku.public_id === skuID)?.assets.filter((asset) => asset.review_status === "approved") ?? [], [assetsQuery.data, skuID]);
  const selectedAssetViews = useMemo(() => new Set(approvedForSKU.filter((asset) => selectedAssets.includes(asset.public_id)).map((asset) => asset.sop_view_key)), [approvedForSKU, selectedAssets]);
  const assetBlockages = useMemo(() => chosenSlots.filter((slot) => slot.kind === "image").map((slot) => ({ slot, missing: list<string>((slot.constraints as Record<string, unknown>).required_views).filter((view) => !selectedAssetViews.has(view)) })).filter(({ missing }) => selectedAssets.length === 0 || missing.length > 0), [chosenSlots, selectedAssetViews, selectedAssets.length]);
  const allAllowPreference = chosenSlots.length > 0 && chosenSlots.every((slot) => (slot.generation_config as Record<string, unknown>).allow_user_extra_prompt === true);

  const createMutation = useMutation({
    mutationFn: () => {
      const generationOverrides = Object.fromEntries(chosenSlots.map((slot) => [slot.slot_key, overrides[slot.slot_key] ?? {}]).filter(([, value]) => Object.keys(value).length));
      const body: components["schemas"]["CreateAIJobRequest"] = {
        sku_id: skuID, template_version_id: versionID, selected_slot_keys: selectedSlots, selected_asset_ids: selectedAssets, locale,
        ...(allAllowPreference && preference.trim() ? { user_preference: preference.trim() } : {}),
        ...(Object.keys(generationOverrides).length ? { generation_overrides: generationOverrides } : {}),
      };
      return apiRequest<Job>("/ai-jobs", { method: "POST", headers: { "Idempotency-Key": idempotencyKey.current }, body: JSON.stringify(body) });
    },
    onSuccess: (job) => router.push(`/ai-jobs/${job.public_id}`),
    onError: () => setError("aiJobCreateError"),
  });

  const steps = [t("aiJobStepSetup"), t("aiJobStepSlots"), t("aiJobStepAssets"), t("aiJobStepOptions"), t("aiJobStepConfirm")];
  const stepTitles = [t("aiJobStepSetup"), t("selectOutputSlots"), t("reviewApprovedAssets"), t("generationOptions"), t("confirmAIJob")];
  const allLoaded = skusQuery.isSuccess && templatesQuery.isSuccess && assetsQuery.isSuccess;
  const loadFailed = skusQuery.isError || templatesQuery.isError || assetsQuery.isError;

  function selectSKU(next: string) {
    setSkuID(next);
    const assets = (assetsQuery.data?.data ?? []).flatMap((category) => category.skus).find((sku) => sku.public_id === next)?.assets ?? [];
    setSelectedAssets(assets.filter((asset) => asset.review_status === "approved").map((asset) => asset.public_id));
  }

  function selectVersion(next: string) {
    setVersionID(next);
    const version = versions.find((entry) => entry.version.public_id === next)?.version;
    const defaults = version?.slots.filter((slot) => slot.default_selected).map((slot) => slot.slot_key) ?? [];
    setSelectedSlots(defaults);
    setOverrides(Object.fromEntries((version?.slots ?? []).map((slot) => [slot.slot_key, initialOverride(slot)])));
    setPreference("");
  }

  function toggleSlot(slot: Slot) {
    setError(null);
    setSelectedSlots((current) => current.includes(slot.slot_key) ? current.filter((key) => key !== slot.slot_key) : [...current, slot.slot_key]);
    setOverrides((current) => ({ ...current, [slot.slot_key]: current[slot.slot_key] ?? initialOverride(slot) }));
  }

  function next() {
    setError(null);
    if (step === 1 && (!skuID || !versionID)) return setError("setupRequired");
    if (step === 2 && selectedSlots.length === 0) return setError("selectSlotRequired");
    if (step === 3 && assetBlockages.length > 0) return setError("requiredAssetViewsMissing");
    setStep((current) => Math.min(5, current + 1));
  }

  function retryOptions() {
    void Promise.all([skusQuery.refetch(), templatesQuery.refetch(), assetsQuery.refetch()]);
  }

  const selectedSKU = skusQuery.data?.data.find((sku) => sku.public_id === skuID);
  const expectedCalls = chosenSlots.reduce((sum, slot) => sum + (overrides[slot.slot_key]?.candidate_count ?? 1), 0);

  return (
    <div className="mx-auto max-w-5xl space-y-6">
      <div className="flex items-start gap-3">
        <Button asChild aria-label={t("backToAIJobs")} className="min-h-11 min-w-11" size="icon" variant="ghost"><Link href="/ai-jobs"><ArrowLeft className="h-4 w-4" /></Link></Button>
        <div><p className="mb-2 text-[11px] font-bold uppercase tracking-[0.16em] text-primary">CargoFlow · New route</p><h1 className="text-3xl font-bold tracking-tight text-navy sm:text-4xl">{t("aiJobWizardTitle")}</h1><p className="mt-2 text-sm text-muted-foreground">{t("aiJobWizardDesc")}</p></div>
      </div>

      <nav aria-label={zh ? "任务创建进度" : "Job creation progress"} className="overflow-x-auto rounded-xl border border-border bg-card p-2 shadow-[var(--shadow-sm)]">
        <ol className="grid min-w-[640px] grid-cols-5 gap-1">
          {steps.map((label, index) => { const number = index + 1; const complete = number < step; const current = number === step; return <li aria-current={current ? "step" : undefined} className={`flex min-h-14 items-center gap-2 rounded-md px-3 text-sm ${current ? "bg-primary text-primary-foreground" : complete ? "bg-primary/10 text-primary" : "text-muted-foreground"}`} key={label}><span className={`flex h-7 w-7 shrink-0 items-center justify-center rounded-full border ${current ? "border-white/50" : "border-current/30"}`}>{complete ? <Check className="h-4 w-4" /> : number}</span><span className="font-medium">{label}</span></li>; })}
        </ol>
      </nav>

      {!allLoaded && !loadFailed ? <div className="h-72 animate-pulse rounded-lg bg-muted" aria-label={zh ? "正在载入任务选项" : "Loading job options"} /> : null}
      {loadFailed ? <div className="flex flex-col items-center gap-3 rounded-lg border border-danger/30 bg-danger/5 p-10 text-center" role="alert"><AlertCircle className="h-6 w-6 text-danger" /><p className="text-sm text-danger">{t("aiJobOptionsLoadError")}</p><Button className="min-h-11" onClick={retryOptions} variant="secondary">{t("retry")}</Button></div> : null}

      {allLoaded ? <Card>
        <CardHeader><CardTitle>{stepTitles[step - 1]}</CardTitle></CardHeader>
        <CardContent className="space-y-6">
          {step === 1 ? <div className="grid gap-5 md:grid-cols-2">
            <div className="space-y-2"><Label htmlFor="job-sku">{t("selectSKU")}</Label><select className={selectClass} id="job-sku" onChange={(event) => selectSKU(event.target.value)} value={skuID}><option value="">{t("chooseSKU")}</option>{skusQuery.data.data.filter((sku) => sku.status === "active").map((sku) => <option key={sku.public_id} value={sku.public_id}>{sku.code} · {sku.product.name}</option>)}</select></div>
            <div className="space-y-2"><Label htmlFor="job-template">{t("selectTemplateVersion")}</Label><select className={selectClass} id="job-template" onChange={(event) => selectVersion(event.target.value)} value={versionID}><option value="">{t("chooseTemplateVersion")}</option>{versions.map(({ template, version }) => <option key={version.public_id} value={version.public_id}>{zh ? template.name_zh : template.name_en} · V{version.version_number}</option>)}</select></div>
            <div className="space-y-2"><Label htmlFor="job-locale">{t("outputLocale")}</Label><select className={selectClass} id="job-locale" onChange={(event) => setLocale(event.target.value)} value={locale}><option value="zh-CN">{t("chineseSimplified")}</option><option value="en">{t("englishOutput")}</option></select></div>
            <div className="rounded-lg border border-border bg-muted/40 p-4"><div className="flex items-center gap-2 text-sm font-medium"><Languages className="h-4 w-4 text-primary" />{selection?.template.target_platform ?? "—"}</div><p className="mt-2 text-sm text-muted-foreground">{selection ? `${zh ? selection.template.name_zh : selection.template.name_en} · V${selection.version.version_number}` : t("chooseTemplateVersion")}</p></div>
          </div> : null}

          {step === 2 ? <fieldset className="space-y-3"><legend className="sr-only">{t("selectOutputSlots")}</legend>{slots.map((slot) => <label className={`flex min-h-16 cursor-pointer items-start gap-3 rounded-lg border p-4 transition-colors ${selectedSlots.includes(slot.slot_key) ? "border-primary bg-primary/5" : "border-border hover:bg-muted/50"}`} key={slot.public_id}><input checked={selectedSlots.includes(slot.slot_key)} className="mt-1 h-5 w-5 accent-[var(--color-primary)]" onChange={() => toggleSlot(slot)} type="checkbox" /><span className="min-w-0 flex-1"><span className="flex flex-wrap items-center gap-2 font-medium">{zh ? slot.name_zh : slot.name_en}<Badge>{slotKind(slot.kind, zh)}</Badge>{slot.optional ? <Badge>{t("optionalSlot")}</Badge> : null}</span><span className="mt-1 block text-sm text-muted-foreground">{zh ? slot.description_zh : slot.description_en}</span></span></label>)}</fieldset> : null}

          {step === 3 ? <div className="space-y-4"><div className="rounded-lg border border-primary/20 bg-primary/5 p-4"><div className="flex items-center gap-2 font-medium"><ImageIcon className="h-4 w-4 text-primary" />{selectedAssets.length} {t("approvedImages")}</div><p className="mt-1 text-sm text-muted-foreground">{t("approvedAssetsHelp")}</p></div>{assetBlockages.length ? <div className="space-y-2 rounded-lg border border-warning/30 bg-warning/5 p-4" role="status"><p className="text-sm font-medium text-warning">{t("requiredAssetViewsMissing")}</p>{assetBlockages.map(({ slot, missing }) => <p className="text-xs text-muted-foreground" key={slot.public_id}>{zh ? slot.name_zh : slot.name_en}: {missing.length ? missing.join(", ") : t("imageAssetRequired")}</p>)}</div> : null}{approvedForSKU.length ? <fieldset className="grid gap-3 sm:grid-cols-2"><legend className="sr-only">{t("reviewApprovedAssets")}</legend>{approvedForSKU.map((asset) => <label className="flex min-h-14 cursor-pointer items-center gap-3 rounded-lg border border-border p-3 hover:bg-muted/50" key={asset.public_id}><input checked={selectedAssets.includes(asset.public_id)} className="h-5 w-5 accent-[var(--color-primary)]" onChange={() => setSelectedAssets((current) => current.includes(asset.public_id) ? current.filter((id) => id !== asset.public_id) : [...current, asset.public_id])} type="checkbox" /><span><span className="block text-sm font-medium">{asset.sop_view_name[zh ? "zh-CN" : "en"]}</span><span className="block text-xs text-muted-foreground">{asset.photo_session_code}</span></span></label>)}</fieldset> : <p className="rounded-lg border border-dashed border-border p-8 text-center text-sm text-muted-foreground">{t("noApprovedAssets")}</p>}</div> : null}

          {step === 4 ? <div className="space-y-4">{chosenSlots.map((slot) => { const config = slot.generation_config as Record<string, unknown>; const candidates = list<number>(config.allowed_candidate_count); const sizes = list<string>(config.allowed_sizes); const qualities = list<string>(config.allowed_qualities); const styles = list<string>(config.allowed_styles); const value = overrides[slot.slot_key] ?? {}; return <section className="rounded-lg border border-border p-4" key={slot.public_id}><div className="flex items-center gap-2"><Settings2 className="h-4 w-4 text-primary" /><h3 className="font-medium">{zh ? slot.name_zh : slot.name_en}</h3></div><div className="mt-4 grid gap-4 sm:grid-cols-2 lg:grid-cols-4">{candidates.length ? <div className="space-y-2"><Label htmlFor={`${slot.slot_key}-candidate`}>{t("candidateCount")}</Label><select className={selectClass} id={`${slot.slot_key}-candidate`} onChange={(event) => setOverrides((current) => ({ ...current, [slot.slot_key]: { ...value, candidate_count: Number(event.target.value) } }))} value={value.candidate_count}>{candidates.map((item) => <option key={item}>{item}</option>)}</select></div> : null}{slot.kind === "image" && sizes.length ? <div className="space-y-2"><Label htmlFor={`${slot.slot_key}-size`}>{t("imageSize")}</Label><select className={selectClass} id={`${slot.slot_key}-size`} onChange={(event) => setOverrides((current) => ({ ...current, [slot.slot_key]: { ...value, size: event.target.value as Override["size"] } }))} value={value.size}>{sizes.map((item) => <option key={item}>{item}</option>)}</select></div> : null}{slot.kind === "image" && qualities.length ? <div className="space-y-2"><Label htmlFor={`${slot.slot_key}-quality`}>{t("imageQuality")}</Label><select className={selectClass} id={`${slot.slot_key}-quality`} onChange={(event) => setOverrides((current) => ({ ...current, [slot.slot_key]: { ...value, quality: event.target.value as Override["quality"] } }))} value={value.quality}>{qualities.map((item) => <option key={item}>{item}</option>)}</select></div> : null}{slot.kind === "image" && styles.length ? <div className="space-y-2"><Label htmlFor={`${slot.slot_key}-style`}>{t("imageStyle")}</Label><select className={selectClass} id={`${slot.slot_key}-style`} onChange={(event) => setOverrides((current) => ({ ...current, [slot.slot_key]: { ...value, style: event.target.value } }))} value={value.style}>{styles.map((item) => <option key={item}>{item}</option>)}</select></div> : null}</div></section>; })}{allAllowPreference ? <div className="space-y-2"><Label htmlFor="job-preference">{t("extraPreference")}</Label><Textarea id="job-preference" maxLength={1000} onChange={(event) => setPreference(event.target.value)} placeholder={t("extraPreferencePlaceholder")} value={preference} /></div> : null}</div> : null}

          {step === 5 ? <div className="grid gap-5 lg:grid-cols-[minmax(0,1fr)_320px]"><div className="space-y-4"><div className="rounded-lg border border-warning/30 bg-warning/5 p-4"><div className="flex items-start gap-3"><ShieldCheck className="mt-0.5 h-5 w-5 text-warning" /><div><p className="font-medium">{t("providerDisclosure")}</p><p className="mt-1 text-sm text-muted-foreground">{t("dryRunNotice")}</p><p className="mt-1 text-sm text-muted-foreground">{t("dryRunExplanation")}</p></div></div></div><section className="rounded-lg border border-border p-4"><h3 className="font-medium">{t("disclosedData")}</h3><ul className="mt-3 space-y-2 text-sm text-muted-foreground"><li className="flex gap-2"><Check className="h-4 w-4 text-primary" />{t("productStructuredData")}</li><li className="flex gap-2"><Check className="h-4 w-4 text-primary" />{t("publishedSOP")}</li><li className="flex gap-2"><Check className="h-4 w-4 text-primary" />{t("selectedApprovedImages")}：{selectedAssets.length}</li></ul></section></div><aside className="rounded-lg border border-border bg-muted/30 p-4"><h3 className="font-medium">{selectedSKU?.code}</h3><p className="mt-1 text-sm text-muted-foreground">{selection && (zh ? selection.template.name_zh : selection.template.name_en)} · V{selection?.version.version_number}</p><dl className="mt-5 space-y-3 text-sm"><div className="flex justify-between gap-3"><dt className="text-muted-foreground">{t("selectedOutputs")}</dt><dd className="font-medium">{chosenSlots.length}</dd></div><div className="flex justify-between gap-3"><dt className="text-muted-foreground">{t("approvedAssets")}</dt><dd className="font-medium">{selectedAssets.length}</dd></div><div className="flex justify-between gap-3"><dt className="text-muted-foreground">{t("expectedCalls")}</dt><dd className="font-medium">{expectedCalls} {t("calls")}</dd></div></dl></aside></div> : null}

          {error ? <p className="rounded-md border border-danger/30 bg-danger/5 px-3 py-2 text-sm text-danger" role="alert">{t(error)}</p> : null}
          <div className="flex flex-col-reverse gap-3 border-t border-border pt-5 sm:flex-row sm:justify-between"><Button className="min-h-11" disabled={step === 1 || createMutation.isPending} onClick={() => { setError(null); setStep((current) => Math.max(1, current - 1)); }} variant="secondary"><ArrowLeft className="h-4 w-4" />{t("previousStep")}</Button>{step < 5 ? <Button className="min-h-11" onClick={next}>{t("nextStep")}<ArrowRight className="h-4 w-4" /></Button> : <Button className="min-h-11" disabled={createMutation.isPending} onClick={() => createMutation.mutate()}>{createMutation.isPending ? t("creatingJob") : t("createJob")}<FileCheck2 className="h-4 w-4" /></Button>}</div>
        </CardContent>
      </Card> : null}
    </div>
  );
}
