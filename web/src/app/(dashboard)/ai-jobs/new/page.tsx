"use client";
/* Authenticated private previews intentionally bypass the Next image optimizer. */
/* eslint-disable @next/next/no-img-element */

import { useMutation, useQuery } from "@tanstack/react-query";
import { AlertCircle, ArrowLeft, ArrowRight, Check, FileCheck2, Image as ImageIcon, Languages, Plus, Settings2, ShieldCheck, Trash2 } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useMemo, useRef, useState } from "react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { apiRequest, authenticatedMediaURL } from "@/lib/api";
import { imageStyleLabel } from "@/lib/image-styles";
import { type MessageKey, useLanguage } from "@/lib/i18n";
import type { components } from "@/lib/openapi-types";

type Template = components["schemas"]["AIContentTemplate"];
type Slot = components["schemas"]["AIContentSlot"];
type Job = components["schemas"]["AIJob"];
type Override = components["schemas"]["AIJobGenerationOverride"];
type SKU = { public_id: string; code: string; status: string; product: { name: string; brand_id?: string; brand?: string } };
type BrandIcon = { public_id: string; name: string; notes: string; media_url: string; status: string };
type AssetCategory = components["schemas"]["AssetReviewCategory"];
type ReviewAsset = AssetCategory["skus"][number]["assets"][number];
type StyleReference = { public_id: string; source_sku_id: string; description_zh: string; description_en: string; derivative_sha256: string; status: string };
type CanvasDraft = { key: string; slotKeys: string[] };
type OptionEntry = { key: string; slot: Slot; requirements?: Slot[]; canvas?: CanvasDraft };

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

function AssetGroup({ assets, label, help, selected, setSelected, zh }: { assets: ReviewAsset[]; label: string; help?: string; selected: string[]; setSelected: (update: (current: string[]) => string[]) => void; zh: boolean }) {
  if (!assets.length) return null;
  return <fieldset className="space-y-3"><legend className="text-sm font-semibold">{label}</legend>{help ? <p className="text-xs leading-5 text-muted-foreground">{help}</p> : null}<div className="grid gap-3 sm:grid-cols-2">{assets.map((asset) => <label className={`flex min-h-14 cursor-pointer items-center gap-3 rounded-lg border p-3 transition-colors ${selected.includes(asset.public_id) ? "border-primary bg-primary/5" : "border-border hover:bg-muted/50"}`} key={asset.public_id}><input checked={selected.includes(asset.public_id)} className="h-5 w-5 accent-[var(--color-primary)]" onChange={() => setSelected((current) => current.includes(asset.public_id) ? current.filter((id) => id !== asset.public_id) : [...current, asset.public_id])} type="checkbox" /><span><span className="block text-sm font-medium">{asset.sop_view_name[zh ? "zh-CN" : "en"]}</span><span className="block text-xs text-muted-foreground">{asset.photo_session_code}</span></span></label>)}</div></fieldset>;
}

export default function NewAIJobPage() {
  const router = useRouter();
  const { language, t } = useLanguage();
  const zh = language === "zh";
  const idempotencyKey = useRef(`ai-job-${crypto.randomUUID()}`);
	const pendingSKUSelection = useRef("");
  const [step, setStep] = useState(1);
  const [skuID, setSkuID] = useState("");
  const [versionID, setVersionID] = useState("");
  const [locale, setLocale] = useState("zh-CN");
  const [selectedSlots, setSelectedSlots] = useState<string[]>([]);
  const [selectedAssets, setSelectedAssets] = useState<string[]>([]);
  const [selectedStyleReferences, setSelectedStyleReferences] = useState<string[]>([]);
	const [selectedBrandIcons, setSelectedBrandIcons] = useState<string[]>([]);
	const [brandIcons, setBrandIcons] = useState<BrandIcon[]>([]);
	const [brandIconsLoading, setBrandIconsLoading] = useState(false);
  const [overrides, setOverrides] = useState<Record<string, Override>>({});
  const [customCanvases, setCustomCanvases] = useState(false);
  const [canvases, setCanvases] = useState<CanvasDraft[]>([]);
  const [canvasOverrides, setCanvasOverrides] = useState<Record<string, Override>>({});
  const [canvasError, setCanvasError] = useState(false);
  const [preference, setPreference] = useState("");
  const [error, setError] = useState<MessageKey | null>(null);

  const skusQuery = useQuery({ queryKey: ["skus", "ai-job-options"], queryFn: () => apiRequest<{ data: SKU[] }>("/skus") });
  const templatesQuery = useQuery({ queryKey: ["ai-content-templates"], queryFn: () => apiRequest<{ data: Template[] }>("/ai-content-templates") });
  const assetsQuery = useQuery({ queryKey: ["assets", "review", "hierarchy", "approved"], queryFn: () => apiRequest<{ data: AssetCategory[] }>("/assets/review/hierarchy?status=approved") });
  const stylesQuery = useQuery({ queryKey: ["style-reference-grants"], queryFn: () => apiRequest<{ data: StyleReference[] }>("/style-reference-grants") });
	const selectedSKUOption = skusQuery.data?.data.find((sku) => sku.public_id === skuID);
	const brandID = selectedSKUOption?.product.brand_id ?? "";
	const brandIconsQuery = { data: { data: brandIcons }, isLoading: brandIconsLoading };

  const versions = useMemo(() => (templatesQuery.data?.data ?? []).flatMap((template) => template.versions.filter((version) => version.status === "published").map((version) => ({ template, version }))), [templatesQuery.data]);
  const selection = versions.find(({ version }) => version.public_id === versionID);
  const slots = useMemo(() => [...(selection?.version.slots ?? [])].sort((a, b) => a.sequence - b.sequence), [selection]);
  const chosenSlots = slots.filter((slot) => selectedSlots.includes(slot.slot_key));
  const chosenImageSlots = chosenSlots.filter((slot) => slot.kind === "image");
  const chosenTextSlots = chosenSlots.filter((slot) => slot.kind !== "image");
  const canvasEntries = canvases.map((canvas) => ({ canvas, requirements: chosenImageSlots.filter((slot) => canvas.slotKeys.includes(slot.slot_key)) }));
  const assignedImageKeys = new Set(canvasEntries.flatMap(({ requirements }) => requirements.map((slot) => slot.slot_key)));
  const unassignedImageSlots = customCanvases ? chosenImageSlots.filter((slot) => !assignedImageKeys.has(slot.slot_key)) : [];
  const canvasConfigurationInvalid = customCanvases && chosenImageSlots.length > 0 && (canvases.length === 0 || canvasEntries.some(({ requirements }) => requirements.length === 0) || unassignedImageSlots.length > 0);
  const optionEntries: OptionEntry[] = customCanvases
    ? [...chosenTextSlots.map((slot) => ({ key: slot.slot_key, slot })), ...canvasEntries.filter(({ requirements }) => requirements.length > 0).map(({ canvas, requirements }) => ({ key: canvas.key, slot: requirements[0], requirements, canvas }))]
    : chosenSlots.map((slot) => ({ key: slot.slot_key, slot }));
  const approvedForSKU = useMemo(() => (assetsQuery.data?.data ?? []).flatMap((category) => category.skus).find((sku) => sku.public_id === skuID)?.assets.filter((asset) => asset.review_status === "approved") ?? [], [assetsQuery.data, skuID]);
  const identityAssets = approvedForSKU.filter((asset) => (asset as ReviewAsset & { origin_type?: string }).origin_type !== "ai_generated");
  const visualAssets = identityAssets.filter((asset) => asset.sop_view_key !== "supplemental_info");
  const informationAssets = identityAssets.filter((asset) => asset.sop_view_key === "supplemental_info");
  const selectedVisualAssets = visualAssets.filter((asset) => selectedAssets.includes(asset.public_id));
  const selectedAssetViews = useMemo(() => new Set(approvedForSKU.filter((asset) => asset.sop_view_key !== "supplemental_info" && selectedAssets.includes(asset.public_id)).map((asset) => asset.sop_view_key)), [approvedForSKU, selectedAssets]);
  const assetBlockages = chosenImageSlots.map((slot) => ({ slot, missing: list<string>((slot.constraints as Record<string, unknown>).required_views).filter((view) => !selectedAssetViews.has(view)) })).filter(({ missing }) => selectedVisualAssets.length === 0 || missing.length > 0);
  const allAllowPreference = chosenSlots.length > 0 && chosenSlots.every((slot) => (slot.generation_config as Record<string, unknown>).allow_user_extra_prompt === true);

  const createMutation = useMutation({
    mutationFn: () => {
      const regularOutputSlots = customCanvases ? chosenTextSlots : chosenSlots;
      const generationOverrides = Object.fromEntries(regularOutputSlots.map((slot) => [slot.slot_key, overrides[slot.slot_key] ?? {}]).filter(([, value]) => Object.keys(value).length));
      const imageCanvases = customCanvases ? canvasEntries.map(({ canvas, requirements }) => {
        const generationOverride = canvasOverrides[canvas.key] ?? initialOverride(requirements[0]);
        return { canvas_key: canvas.key, slot_keys: requirements.map((slot) => slot.slot_key), ...(Object.keys(generationOverride).length ? { generation_override: generationOverride } : {}) };
      }) : [];
      const body: components["schemas"]["CreateAIJobRequest"] = {
        sku_id: skuID,
        template_version_id: versionID,
        selected_slot_keys: selectedSlots,
        selected_asset_ids: selectedAssets,
        selected_style_reference_ids: selectedStyleReferences,
		selected_brand_icon_ids: chosenImageSlots.length ? selectedBrandIcons : [],
        locale,
        ...(allAllowPreference && preference.trim() ? { user_preference: preference.trim() } : {}),
        ...(Object.keys(generationOverrides).length ? { generation_overrides: generationOverrides } : {}),
        ...(imageCanvases.length ? { image_canvases: imageCanvases } : {}),
      };
      return apiRequest<Job>("/ai-jobs", { method: "POST", headers: { "Idempotency-Key": idempotencyKey.current }, body: JSON.stringify(body) });
    },
    onSuccess: (job) => router.push(`/ai-jobs/${job.public_id}`),
    onError: () => setError("aiJobCreateError"),
  });

  const steps = [t("aiJobStepSetup"), t("aiJobStepSlots"), t("aiJobStepAssets"), t("aiJobStepOptions"), t("aiJobStepConfirm")];
  const stepTitles = [t("aiJobStepSetup"), t("selectOutputSlots"), t("reviewApprovedAssets"), t("generationOptions"), t("confirmAIJob")];
	// Style grants are optional; a temporary failure must not block ordinary
	// same-SKU text or image task creation.
  const allLoaded = skusQuery.isSuccess && templatesQuery.isSuccess && assetsQuery.isSuccess;
  const loadFailed = skusQuery.isError || templatesQuery.isError || assetsQuery.isError;

  async function selectSKU(next: string) {
		pendingSKUSelection.current = next;
    setSkuID(next);
    const assets = (assetsQuery.data?.data ?? []).flatMap((category) => category.skus).find((sku) => sku.public_id === next)?.assets ?? [];
		setSelectedAssets(assets.filter((asset) => asset.review_status === "approved" && (asset as ReviewAsset & { origin_type?: string }).origin_type !== "ai_generated").map((asset) => asset.public_id));
		setSelectedBrandIcons([]);
		setBrandIcons([]);
		const nextBrandID = skusQuery.data?.data.find((sku) => sku.public_id === next)?.product.brand_id;
		if (nextBrandID) {
			setBrandIconsLoading(true);
			try { const response = await apiRequest<{ data: BrandIcon[] }>(`/brands/${nextBrandID}/icons?status=active`); if (pendingSKUSelection.current === next) { setBrandIcons(response.data); setSelectedBrandIcons(response.data.map((icon) => icon.public_id)); } } finally { if (pendingSKUSelection.current === next) setBrandIconsLoading(false); }
		}
  }

  function selectVersion(next: string) {
    setVersionID(next);
    const version = versions.find((entry) => entry.version.public_id === next)?.version;
    const defaults = version?.slots.filter((slot) => slot.default_selected).map((slot) => slot.slot_key) ?? [];
    setSelectedSlots(defaults);
    setOverrides(Object.fromEntries((version?.slots ?? []).map((slot) => [slot.slot_key, initialOverride(slot)])));
    setCustomCanvases(false);
    setCanvases([]);
    setCanvasOverrides({});
    setCanvasError(false);
    setPreference("");
  }

  function toggleSlot(slot: Slot) {
    setError(null);
    setCanvasError(false);
    const removing = selectedSlots.includes(slot.slot_key);
    if (removing && slot.kind === "image") {
      setCanvases((current) => current.map((canvas) => ({ ...canvas, slotKeys: canvas.slotKeys.filter((key) => key !== slot.slot_key) })));
      if (chosenImageSlots.length === 1) {
        setCustomCanvases(false);
        setCanvases([]);
        setCanvasOverrides({});
      }
    }
    setSelectedSlots((current) => removing ? current.filter((key) => key !== slot.slot_key) : [...current, slot.slot_key]);
    setOverrides((current) => ({ ...current, [slot.slot_key]: current[slot.slot_key] ?? initialOverride(slot) }));
  }

  function enableCustomCanvases() {
    setCustomCanvases(true);
    setCanvasError(false);
    if (canvases.length === 0 && chosenImageSlots.length > 0) addCanvas();
  }

  function addCanvas() {
    const assigned = new Set(canvases.flatMap((canvas) => canvas.slotKeys));
    const initialSlot = chosenImageSlots.find((slot) => !assigned.has(slot.slot_key)) ?? chosenImageSlots[0];
    if (!initialSlot) return;
    const key = `canvas-${crypto.randomUUID()}`;
    setCanvases((current) => [...current, { key, slotKeys: [initialSlot.slot_key] }]);
    setCanvasOverrides((current) => ({ ...current, [key]: initialOverride(initialSlot) }));
    setCanvasError(false);
  }

  function removeCanvas(key: string) {
    setCanvases((current) => current.filter((canvas) => canvas.key !== key));
    setCanvasOverrides((current) => Object.fromEntries(Object.entries(current).filter(([canvasKey]) => canvasKey !== key)));
    setCanvasError(false);
  }

  function toggleCanvasSlot(canvasKey: string, slot: Slot) {
    const target = canvases.find((canvas) => canvas.key === canvasKey);
    if (!target) return;
    const slotKeys = target.slotKeys.includes(slot.slot_key) ? target.slotKeys.filter((key) => key !== slot.slot_key) : [...target.slotKeys, slot.slot_key];
    const primary = chosenImageSlots.find((imageSlot) => slotKeys.includes(imageSlot.slot_key));
    setCanvases((current) => current.map((canvas) => canvas.key === canvasKey ? { ...canvas, slotKeys } : canvas));
    if (primary) setCanvasOverrides((values) => ({ ...values, [canvasKey]: initialOverride(primary) }));
    setCanvasError(false);
  }

  function next() {
    setError(null);
    setCanvasError(false);
    if (step === 1 && (!skuID || !versionID)) return setError("setupRequired");
    if (step === 2 && selectedSlots.length === 0) return setError("selectSlotRequired");
    if (step === 2 && canvasConfigurationInvalid) return setCanvasError(true);
    if (step === 3 && assetBlockages.length > 0) return setError("requiredAssetViewsMissing");
    setStep((current) => Math.min(5, current + 1));
  }

  function retryOptions() {
    void Promise.all([skusQuery.refetch(), templatesQuery.refetch(), assetsQuery.refetch()]);
  }

  const selectedSKU = skusQuery.data?.data.find((sku) => sku.public_id === skuID);
  const expectedCalls = optionEntries.reduce((sum, entry) => sum + ((entry.canvas ? canvasOverrides[entry.key] : overrides[entry.slot.slot_key])?.candidate_count ?? 1), 0);
  const outputCount = optionEntries.length;

  return <div className="mx-auto max-w-5xl space-y-6">
    <div className="flex items-start gap-3">
      <Button asChild aria-label={t("backToAIJobs")} className="min-h-11 min-w-11" size="icon" variant="ghost"><Link href="/ai-jobs"><ArrowLeft className="h-4 w-4" /></Link></Button>
      <div><p className="mb-2 text-[11px] font-bold uppercase tracking-[0.16em] text-primary">CargoFlows · New route</p><h1 className="text-3xl font-bold tracking-tight text-navy sm:text-4xl">{t("aiJobWizardTitle")}</h1><p className="mt-2 text-sm text-muted-foreground">{t("aiJobWizardDesc")}</p></div>
    </div>

    <nav aria-label={zh ? "任务创建进度" : "Job creation progress"} className="overflow-x-auto rounded-xl border border-border bg-card p-2 shadow-[var(--shadow-sm)]">
      <ol className="grid min-w-[640px] grid-cols-5 gap-1">{steps.map((label, index) => { const number = index + 1; const complete = number < step; const current = number === step; return <li aria-current={current ? "step" : undefined} className={`flex min-h-14 items-center gap-2 rounded-md px-3 text-sm ${current ? "bg-primary text-primary-foreground" : complete ? "bg-primary/10 text-primary" : "text-muted-foreground"}`} key={label}><span className={`flex h-7 w-7 shrink-0 items-center justify-center rounded-full border ${current ? "border-white/50" : "border-current/30"}`}>{complete ? <Check className="h-4 w-4" /> : number}</span><span className="font-medium">{label}</span></li>; })}</ol>
    </nav>

    {!allLoaded && !loadFailed ? <div className="h-72 animate-pulse rounded-lg bg-muted" aria-label={zh ? "正在载入任务选项" : "Loading job options"} /> : null}
    {loadFailed ? <div className="flex flex-col items-center gap-3 rounded-lg border border-danger/30 bg-danger/5 p-10 text-center" role="alert"><AlertCircle className="h-6 w-6 text-danger" /><p className="text-sm text-danger">{t("aiJobOptionsLoadError")}</p><Button className="min-h-11" onClick={retryOptions} variant="secondary">{t("retry")}</Button></div> : null}

    {allLoaded ? <Card><CardHeader><CardTitle>{stepTitles[step - 1]}</CardTitle></CardHeader><CardContent className="space-y-6">
      {step === 1 ? <div className="grid gap-5 md:grid-cols-2">
        <div className="space-y-2"><Label htmlFor="job-sku">{t("selectSKU")}</Label><select className={selectClass} id="job-sku" onChange={(event) => selectSKU(event.target.value)} value={skuID}><option value="">{t("chooseSKU")}</option>{skusQuery.data.data.filter((sku) => sku.status === "active").map((sku) => <option key={sku.public_id} value={sku.public_id}>{sku.code} · {sku.product.name}</option>)}</select></div>
        <div className="space-y-2"><Label htmlFor="job-template">{t("selectTemplateVersion")}</Label><select className={selectClass} id="job-template" onChange={(event) => selectVersion(event.target.value)} value={versionID}><option value="">{t("chooseTemplateVersion")}</option>{versions.map(({ template, version }) => <option key={version.public_id} value={version.public_id}>{zh ? template.name_zh : template.name_en} · V{version.version_number}</option>)}</select></div>
        <div className="space-y-2"><Label htmlFor="job-locale">{t("outputLocale")}</Label><select className={selectClass} id="job-locale" onChange={(event) => setLocale(event.target.value)} value={locale}><option value="zh-CN">{t("chineseSimplified")}</option><option value="en">{t("englishOutput")}</option></select></div>
        <div className="rounded-lg border border-border bg-muted/40 p-4"><div className="flex items-center gap-2 text-sm font-medium"><Languages className="h-4 w-4 text-primary" />{selection?.template.target_platform ?? "—"}</div><p className="mt-2 text-sm text-muted-foreground">{selection ? `${zh ? selection.template.name_zh : selection.template.name_en} · V${selection.version.version_number}` : t("chooseTemplateVersion")}</p></div>
      </div> : null}

      {step === 2 ? <div className="space-y-5">
        <fieldset className="space-y-3"><legend className="sr-only">{t("selectOutputSlots")}</legend>{slots.map((slot) => <label className={`flex min-h-16 cursor-pointer items-start gap-3 rounded-lg border p-4 transition-colors ${selectedSlots.includes(slot.slot_key) ? "border-primary bg-primary/5" : "border-border hover:bg-muted/50"}`} key={slot.public_id}><input checked={selectedSlots.includes(slot.slot_key)} className="mt-1 h-5 w-5 accent-[var(--color-primary)]" onChange={() => toggleSlot(slot)} type="checkbox" /><span className="min-w-0 flex-1"><span className="flex flex-wrap items-center gap-2 font-medium">{zh ? slot.name_zh : slot.name_en}<Badge>{slotKind(slot.kind, zh)}</Badge>{slot.optional ? <Badge>{t("optionalSlot")}</Badge> : null}</span><span className="mt-1 block text-sm text-muted-foreground">{zh ? slot.description_zh : slot.description_en}</span></span></label>)}</fieldset>
        {chosenImageSlots.length ? <fieldset className="rounded-xl border border-primary/25 bg-primary/[0.035] p-4"><legend className="px-1 text-sm font-semibold text-navy">{zh ? "图片画布" : "Image canvases"}</legend><p className="mt-1 text-sm text-muted-foreground">{zh ? "可以添加多张画布，每张画布自由选择图片项目；同一项目可用于多张画布。" : "Add multiple canvases and choose projects independently for each. A project may be reused across canvases."}</p>
          <div className="mt-4 grid gap-3 sm:grid-cols-2">
            <label className={`cursor-pointer rounded-lg border p-4 ${!customCanvases ? "border-primary bg-card shadow-[var(--shadow-sm)]" : "border-border bg-card/60"}`}><span className="flex items-start gap-3"><input checked={!customCanvases} className="mt-1 h-5 w-5 accent-[var(--color-primary)]" name="image-output-mode" onChange={() => { setCustomCanvases(false); setCanvasError(false); }} type="radio" /><span><span className="block font-medium">{zh ? "每个项目单独一张" : "One image per project"}</span><span className="mt-1 block text-xs leading-5 text-muted-foreground">{chosenImageSlots.length} {zh ? "张图片输出" : "image outputs"}</span></span></span></label>
            <label className={`cursor-pointer rounded-lg border p-4 ${customCanvases ? "border-primary bg-card shadow-[var(--shadow-sm)]" : "border-border bg-card/60"}`}><span className="flex items-start gap-3"><input checked={customCanvases} className="mt-1 h-5 w-5 accent-[var(--color-primary)]" name="image-output-mode" onChange={enableCustomCanvases} type="radio" /><span><span className="block font-medium">{zh ? "自由编排多张画布" : "Build multiple canvases"}</span><span className="mt-1 block text-xs leading-5 text-muted-foreground">{zh ? "每张画布生成一张图片" : "Each canvas creates one image"}</span></span></span></label>
          </div>
          {customCanvases ? <div className="mt-5 space-y-3">
            <div className="flex flex-wrap items-center justify-between gap-3"><div><p className="text-sm font-semibold text-navy">{zh ? "画布输出清单" : "Canvas output list"}</p><p className="mt-1 text-xs text-muted-foreground">{zh ? "画布顺序就是生成结果顺序。" : "Canvas order is the output order."}</p></div><Button className="min-h-10" onClick={addCanvas} type="button" variant="secondary"><Plus className="h-4 w-4" />{zh ? "添加画布" : "Add canvas"}</Button></div>
            {canvasEntries.map(({ canvas, requirements }, index) => <article className="overflow-hidden rounded-xl border border-border bg-card" key={canvas.key}><header className="flex items-center justify-between gap-3 border-b border-border bg-muted/35 px-4 py-3"><div className="flex items-center gap-3"><span className="flex h-8 w-8 items-center justify-center rounded-md bg-primary text-sm font-bold text-primary-foreground">{index + 1}</span><div><h3 className="text-sm font-semibold">{zh ? `画布 ${index + 1}` : `Canvas ${index + 1}`}</h3><p className="text-xs text-muted-foreground">{requirements.length} {zh ? "个图片项目" : "image projects"}</p></div></div><Button aria-label={zh ? `删除画布 ${index + 1}` : `Delete canvas ${index + 1}`} className="min-h-10 min-w-10" onClick={() => removeCanvas(canvas.key)} size="icon" type="button" variant="ghost"><Trash2 className="h-4 w-4 text-danger" /></Button></header><div className="grid gap-2 p-4 sm:grid-cols-2">{chosenImageSlots.map((slot) => { const checked = canvas.slotKeys.includes(slot.slot_key); return <label className={`flex cursor-pointer items-start gap-3 rounded-lg border p-3 transition-colors ${checked ? "border-primary bg-primary/5" : "border-border hover:bg-muted/40"}`} key={slot.slot_key}><input checked={checked} className="mt-0.5 h-5 w-5 accent-[var(--color-primary)]" onChange={() => toggleCanvasSlot(canvas.key, slot)} type="checkbox" /><span className="text-sm"><span className="block font-medium">{zh ? slot.name_zh : slot.name_en}</span><span className="mt-1 block text-xs leading-5 text-muted-foreground">{zh ? slot.description_zh : slot.description_en}</span></span></label>; })}</div></article>)}
            {canvases.length === 0 ? <button className="w-full rounded-xl border border-dashed border-primary/35 bg-card px-4 py-8 text-sm text-primary hover:bg-primary/5" onClick={addCanvas} type="button"><Plus className="mx-auto mb-2 h-5 w-5" />{zh ? "添加第一张画布" : "Add the first canvas"}</button> : null}
            {canvasError ? <div className="rounded-lg border border-danger/30 bg-danger/5 p-3 text-sm text-danger" role="alert">{zh ? "每张画布至少选择一个项目，并确保所有已选图片项目至少出现在一张画布中。" : "Choose at least one project per canvas and place every selected image project on at least one canvas."}</div> : null}
            {unassignedImageSlots.length ? <p className="text-xs text-warning">{zh ? "尚未放入画布：" : "Not placed on a canvas: "}{unassignedImageSlots.map((slot) => zh ? slot.name_zh : slot.name_en).join("、")}</p> : null}
          </div> : null}
        </fieldset> : null}
      </div> : null}

      {step === 3 ? <div className="space-y-4"><div className="rounded-lg border border-primary/20 bg-primary/5 p-4"><div className="flex items-center gap-2 font-medium"><ImageIcon className="h-4 w-4 text-primary" />{selectedAssets.length} {t("approvedImages")}</div><p className="mt-1 text-sm text-muted-foreground">{t("approvedAssetsHelp")}</p></div>{assetBlockages.length ? <div className="space-y-2 rounded-lg border border-warning/30 bg-warning/5 p-4" role="status"><p className="text-sm font-medium text-warning">{t("requiredAssetViewsMissing")}</p>{assetBlockages.map(({ slot, missing }) => <p className="text-xs text-muted-foreground" key={slot.public_id}>{zh ? slot.name_zh : slot.name_en}: {missing.length ? missing.join(", ") : t("imageAssetRequired")}</p>)}</div> : null}{identityAssets.length ? <div className="space-y-5"><AssetGroup assets={visualAssets} label={zh ? "目标 SKU 身份素材" : "Target SKU identity assets"} selected={selectedAssets} setSelected={setSelectedAssets} zh={zh} /><AssetGroup assets={informationAssets} help={zh ? "仅作为卖点、规格和说明书中的可见事实来源，不作为商品外观或风格参考。" : "Used only as a factual source for visible specifications, selling points, and manual content—not as an appearance or style reference."} label={zh ? "补充资料" : "Supplemental information"} selected={selectedAssets} setSelected={setSelectedAssets} zh={zh} /></div> : <p className="rounded-lg border border-dashed border-border p-8 text-center text-sm text-muted-foreground">{t("noApprovedAssets")}</p>}{chosenImageSlots.length ? <fieldset className="space-y-3 rounded-lg border border-primary/25 bg-primary/[0.025] p-4"><legend className="px-1 text-sm font-semibold">{zh?"品牌图标参考（可选）":"Brand mark references (optional)"}</legend><p className="text-xs leading-5 text-muted-foreground">{zh?"默认选择全部启用图标。生成时保持图形与文字结构，颜色可以随所选风格和背景调整。":"All active marks are selected by default. Shape and lettering stay fixed; colors may adapt to the selected style and background."}</p><div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">{(brandIconsQuery.data?.data??[]).map((icon)=><label className={`cursor-pointer overflow-hidden rounded-lg border bg-card ${selectedBrandIcons.includes(icon.public_id)?"border-primary ring-2 ring-primary/10":"border-border"}`} key={icon.public_id}><span className="grid aspect-[3/2] place-items-center bg-muted/35 p-3"><img alt={icon.name} className="max-h-full max-w-full object-contain" src={authenticatedMediaURL(icon.media_url)}/></span><span className="flex items-start gap-2 border-t border-border p-3"><input checked={selectedBrandIcons.includes(icon.public_id)} className="mt-0.5 h-5 w-5 accent-[var(--color-primary)]" onChange={()=>setSelectedBrandIcons((current)=>current.includes(icon.public_id)?current.filter((id)=>id!==icon.public_id):[...current,icon.public_id])} type="checkbox"/><span className="text-sm font-medium">{icon.name}</span></span></label>)}</div>{brandID&&!brandIconsQuery.isLoading&&!brandIconsQuery.data?.data.length?<p className="text-sm text-muted-foreground">{zh?"该品牌暂无启用的品牌图标。":"This brand has no active marks."}</p>:null}{!brandID?<p className="text-sm text-muted-foreground">{zh?"该 SKU 尚未关联品牌。":"This SKU is not linked to a brand."}</p>:null}</fieldset>:null}<fieldset className="space-y-3 rounded-lg border border-border p-4"><legend className="px-1 text-sm font-semibold">{zh ? "跨 SKU 风格参考（可选）" : "Cross-SKU style references (optional)"}</legend><p className="text-xs leading-5 text-muted-foreground">{zh ? "仅传递已审核派生图中的背景、灯光、构图和氛围；来源商品主体已被排除，不能作为身份或事实依据。" : "Only approved derivatives may transfer background, lighting, composition, and atmosphere. Source-product identity is excluded and never establishes facts."}</p><div className="grid gap-2 sm:grid-cols-2">{(stylesQuery.data?.data ?? []).map((style) => <label className={`flex min-h-14 cursor-pointer items-start gap-3 rounded-lg border p-3 ${selectedStyleReferences.includes(style.public_id) ? "border-primary bg-primary/5" : "border-border"}`} key={style.public_id}><input checked={selectedStyleReferences.includes(style.public_id)} className="mt-0.5 h-5 w-5" onChange={() => setSelectedStyleReferences((current) => current.includes(style.public_id) ? current.filter((id) => id !== style.public_id) : [...current, style.public_id])} type="checkbox" /><span className="text-sm">{zh ? style.description_zh : style.description_en}</span></label>)}</div>{stylesQuery.data?.data.length === 0 ? <p className="text-sm text-muted-foreground">{zh ? "暂无管理员审核通过的风格参考。" : "No administrator-approved style references yet."}</p> : null}</fieldset><section className="rounded-lg border border-border bg-muted/30 p-4"><h3 className="text-sm font-semibold">{zh ? "型号组结构参考" : "Model-family structure references"}</h3><p className="mt-1 text-xs leading-5 text-muted-foreground">{zh ? "系统会自动解析同型号组内已批准的灰度结构派生图；颜色、标签、接口、控制件、配件和包装始终禁止继承。" : "The server automatically resolves approved grayscale derivatives from the same model family. Color, labels, ports, controls, accessories, and packaging are always forbidden."}</p></section></div> : null}

      {step === 4 ? <div className="space-y-4">{optionEntries.map((entry, index) => {
        const { slot } = entry;
        const config = slot.generation_config as Record<string, unknown>;
        const candidates = list<number>(config.allowed_candidate_count);
        const sizes = list<string>(config.allowed_sizes);
        const qualities = list<string>(config.allowed_qualities);
        const styles = list<string>(config.allowed_styles);
        const value = entry.canvas ? (canvasOverrides[entry.key] ?? initialOverride(slot)) : (overrides[slot.slot_key] ?? {});
        const update = (next: Override) => entry.canvas ? setCanvasOverrides((current) => ({ ...current, [entry.key]: next })) : setOverrides((current) => ({ ...current, [slot.slot_key]: next }));
        return <section className="rounded-lg border border-border p-4" key={entry.key}><div className="flex flex-wrap items-center gap-2"><Settings2 className="h-4 w-4 text-primary" /><h3 className="font-medium">{entry.canvas ? (zh ? `画布 ${index - chosenTextSlots.length + 1} 设置` : `Canvas ${index - chosenTextSlots.length + 1} settings`) : (zh ? slot.name_zh : slot.name_en)}</h3>{entry.requirements ? <Badge>{entry.requirements.length} {zh ? "个项目" : "projects"}</Badge> : null}</div>{entry.requirements ? <div className="mt-3 flex flex-wrap gap-2">{entry.requirements.map((item) => <Badge key={item.slot_key}>{zh ? item.name_zh : item.name_en}</Badge>)}</div> : null}<div className="mt-4 grid gap-4 sm:grid-cols-2 lg:grid-cols-4">{candidates.length ? <div className="space-y-2"><Label htmlFor={`${entry.key}-candidate`}>{t("candidateCount")}</Label><select className={selectClass} id={`${entry.key}-candidate`} onChange={(event) => update({ ...value, candidate_count: Number(event.target.value) })} value={value.candidate_count}>{candidates.map((item) => <option key={item}>{item}</option>)}</select></div> : null}{slot.kind === "image" && sizes.length ? <div className="space-y-2"><Label htmlFor={`${entry.key}-size`}>{t("imageSize")}</Label><select className={selectClass} id={`${entry.key}-size`} onChange={(event) => update({ ...value, size: event.target.value as Override["size"] })} value={value.size}>{sizes.map((item) => <option key={item}>{item}</option>)}</select></div> : null}{slot.kind === "image" && qualities.length ? <div className="space-y-2"><Label htmlFor={`${entry.key}-quality`}>{t("imageQuality")}</Label><select className={selectClass} id={`${entry.key}-quality`} onChange={(event) => update({ ...value, quality: event.target.value as Override["quality"] })} value={value.quality}>{qualities.map((item) => <option key={item}>{item}</option>)}</select></div> : null}{slot.kind === "image" && styles.length ? <div className="space-y-2"><Label htmlFor={`${entry.key}-style`}>{t("imageStyle")}</Label><select className={selectClass} id={`${entry.key}-style`} onChange={(event) => update({ ...value, style: event.target.value })} value={value.style}>{styles.map((item) => { const label = imageStyleLabel(item, zh ? "zh" : "en"); return <option key={item} value={item}>{label}{label === item ? ` · ${zh ? "旧版/自定义" : "Legacy/custom"}` : ""}</option>; })}</select></div> : null}</div></section>;
      })}{allAllowPreference ? <div className="space-y-2"><Label htmlFor="job-preference">{t("extraPreference")}</Label><Textarea id="job-preference" maxLength={1000} onChange={(event) => setPreference(event.target.value)} placeholder={t("extraPreferencePlaceholder")} value={preference} /></div> : null}</div> : null}

      {step === 5 ? <div className="grid gap-5 lg:grid-cols-[minmax(0,1fr)_320px]"><div className="space-y-4"><div className="rounded-lg border border-warning/30 bg-warning/5 p-4"><div className="flex items-start gap-3"><ShieldCheck className="mt-0.5 h-5 w-5 text-warning" /><div><p className="font-medium">{t("providerDisclosure")}</p><p className="mt-1 text-sm text-muted-foreground">{t("dryRunNotice")}</p><p className="mt-1 text-sm text-muted-foreground">{t("dryRunExplanation")}</p></div></div></div><section className="rounded-lg border border-border p-4"><h3 className="font-medium">{t("disclosedData")}</h3><ul className="mt-3 space-y-2 text-sm text-muted-foreground"><li className="flex gap-2"><Check className="h-4 w-4 text-primary" />{t("productStructuredData")}</li><li className="flex gap-2"><Check className="h-4 w-4 text-primary" />{t("publishedSOP")}</li><li className="flex gap-2"><Check className="h-4 w-4 text-primary" />{t("selectedApprovedImages")}：{selectedAssets.length}</li>{selectedBrandIcons.length?<li className="flex gap-2"><Check className="h-4 w-4 text-primary" />{zh?`品牌图标参考：${selectedBrandIcons.length}`:`Brand mark references: ${selectedBrandIcons.length}`}</li>:null}{customCanvases ? <li className="flex gap-2"><Check className="h-4 w-4 text-primary" />{zh ? `${canvases.length} 张自定义画布，将生成 ${canvases.length} 张图片` : `${canvases.length} custom canvases will create ${canvases.length} images`}</li> : null}</ul></section></div><aside className="rounded-lg border border-border bg-muted/30 p-4"><h3 className="font-medium">{selectedSKU?.code}</h3><p className="mt-1 text-sm text-muted-foreground">{selection && (zh ? selection.template.name_zh : selection.template.name_en)} · V{selection?.version.version_number}</p><dl className="mt-5 space-y-3 text-sm"><div className="flex justify-between gap-3"><dt className="text-muted-foreground">{t("selectedOutputs")}</dt><dd className="font-medium">{outputCount}</dd></div><div className="flex justify-between gap-3"><dt className="text-muted-foreground">{t("approvedAssets")}</dt><dd className="font-medium">{selectedAssets.length}</dd></div><div className="flex justify-between gap-3"><dt className="text-muted-foreground">{zh?"品牌图标":"Brand marks"}</dt><dd className="font-medium">{selectedBrandIcons.length}</dd></div><div className="flex justify-between gap-3"><dt className="text-muted-foreground">{t("expectedCalls")}</dt><dd className="font-medium">{expectedCalls} {t("calls")}</dd></div></dl></aside></div> : null}

      {error ? <p className="rounded-md border border-danger/30 bg-danger/5 px-3 py-2 text-sm text-danger" role="alert">{t(error)}</p> : null}
      <div className="flex flex-col-reverse gap-3 border-t border-border pt-5 sm:flex-row sm:justify-between"><Button className="min-h-11" disabled={step === 1 || createMutation.isPending} onClick={() => { setError(null); setCanvasError(false); setStep((current) => Math.max(1, current - 1)); }} variant="secondary"><ArrowLeft className="h-4 w-4" />{t("previousStep")}</Button>{step < 5 ? <Button className="min-h-11" onClick={next}>{t("nextStep")}<ArrowRight className="h-4 w-4" /></Button> : <Button className="min-h-11" disabled={createMutation.isPending} onClick={() => createMutation.mutate()}>{createMutation.isPending ? t("creatingJob") : t("createJob")}<FileCheck2 className="h-4 w-4" /></Button>}</div>
    </CardContent></Card> : null}
  </div>;
}
