"use client";

import { useQuery } from "@tanstack/react-query";
import {
  Camera,
  Check,
  ChevronRight,
  CircleAlert,
  CloudUpload,
  ImageIcon,
  LoaderCircle,
  LockKeyhole,
  RotateCcw,
} from "lucide-react";
import Link from "next/link";
import { useEffect, useMemo, useRef, useState } from "react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { apiRequest, authenticatedMediaURL } from "@/lib/api";
import { CaptureImageError, normalizeCaptureImage } from "@/lib/capture-image";
import { useLanguage } from "@/lib/i18n";
import type { components } from "@/lib/openapi-types";
import { localizedText } from "@/lib/sop";
import { cn } from "@/lib/utils";

type SKU = components["schemas"]["SKU"];
type SOPSummary = components["schemas"]["CaptureSOPSummary"];
type SOPVersion = components["schemas"]["SOPVersion"];
type SOPView = components["schemas"]["SOPView"];
type PhotoSession = components["schemas"]["PhotoSession"];
type UploadEnvelope = components["schemas"]["AssetUploadEnvelope"];
type CompletedAsset = components["schemas"]["CompletedAsset"];

type UploadStatus = "optimizing" | "uploading" | "complete" | "error";

interface UploadItem {
  id: string;
  file: File;
  previewURL: string;
  status: UploadStatus;
  error?: string;
  asset?: CompletedAsset;
}

const copy = {
  zh: {
    eyebrow: "CargoFlows · Capture deck",
    title: "素材采集",
    description: "选择一个 SKU，按照已发布 SOP 逐项拍摄。完成的图片会直接进入素材审核。",
    setup: "设置拍摄任务",
    selectSKU: "选择 SKU",
    chooseSKU: "请选择启用中的 SKU",
    selectSOP: "选择 SOP 版本",
    chooseSOP: "请选择已发布 SOP",
    loading: "正在载入采集资料…",
    loadError: "无法载入采集资料，请重试。",
    noSKU: "没有可用于拍摄的启用 SKU。",
    noSOP: "这个 SKU 的分类还没有已发布 SOP。请先发布拍摄 SOP。",
    sessionLocked: "批次已创建，SKU 与 SOP 已锁定",
    shotList: "拍摄清单",
    requiredProgress: "必拍进度",
    required: "必拍",
    optional: "选拍",
    reference: "基准",
    standard: "标准",
    detail: "细节",
    multiple: "可上传多张",
    referenceImages: "参考图",
    referenceAlt: "拍摄参考图",
    drop: "拖放图片到这里",
    chooseFiles: "选择图片",
    takePhoto: "手机拍照",
    optimizing: "正在优化",
    uploading: "正在上传",
    complete: "已完成",
    failed: "上传失败",
    retry: "重试",
    session: "拍摄批次",
    finish: "完成并查看素材",
    finishHelp: "所有必拍视角完成后即可进入素材审核。",
    idleHelp: "支持 JPEG、PNG、WebP 及浏览器可读取的手机照片；上传前会自动优化。",
    uploaded: "图片已上传并进入待审核状态。",
    decode_failed: "无法读取这张图片。请改用 JPEG、PNG 或 WebP。",
    canvas_failed: "浏览器无法处理这张图片，请更换图片后重试。",
    too_large: "优化后的图片仍超过 50MB，请选择较小的图片。",
    upload_failed: "上传未完成。请检查网络后重试。",
  },
  en: {
    eyebrow: "CargoFlows · Capture deck",
    title: "Asset capture",
    description: "Choose a SKU and work through its published SOP. Completed images go straight to asset review.",
    setup: "Set up capture",
    selectSKU: "Select SKU",
    chooseSKU: "Choose an active SKU",
    selectSOP: "Select SOP version",
    chooseSOP: "Choose a published SOP",
    loading: "Loading capture details…",
    loadError: "Capture details could not be loaded. Try again.",
    noSKU: "There are no active SKUs available for capture.",
    noSOP: "This SKU category has no published SOP. Publish a capture SOP first.",
    sessionLocked: "The batch is created; its SKU and SOP are locked",
    shotList: "Shot checklist",
    requiredProgress: "Required progress",
    required: "Required",
    optional: "Optional",
    reference: "Reference",
    standard: "Standard",
    detail: "Detail",
    multiple: "Multiple images allowed",
    referenceImages: "References",
    referenceAlt: "Capture reference",
    drop: "Drop images here",
    chooseFiles: "Choose images",
    takePhoto: "Take photo",
    optimizing: "Optimizing",
    uploading: "Uploading",
    complete: "Complete",
    failed: "Upload failed",
    retry: "Retry",
    session: "Photo batch",
    finish: "Finish and review assets",
    finishHelp: "Complete every required view to continue to asset review.",
    idleHelp: "Supports JPEG, PNG, WebP, and phone photos your browser can read. Images are optimized before upload.",
    uploaded: "Image uploaded and queued for review.",
    decode_failed: "This image could not be read. Use JPEG, PNG, or WebP instead.",
    canvas_failed: "The browser could not process this image. Choose another image and try again.",
    too_large: "The optimized image is still over 50MB. Choose a smaller image.",
    upload_failed: "The upload did not finish. Check your connection and retry.",
  },
} as const;

function uploadID() {
  return typeof crypto !== "undefined" && "randomUUID" in crypto
    ? crypto.randomUUID()
    : `upload-${Date.now()}-${Math.random().toString(16).slice(2)}`;
}

function versionTimestamp(version: SOPVersion) {
  return Date.parse(version.published_at ?? version.updated_at) || 0;
}

interface CaptureViewCardProps {
  view: SOPView;
  index: number;
  items: UploadItem[];
  disabled: boolean;
  language: "zh" | "en";
  onFiles: (files: File[]) => void;
  onRetry: (item: UploadItem) => void;
}

function CaptureViewCard({ view, index, items, disabled, language, onFiles, onRetry }: CaptureViewCardProps) {
  const c = copy[language];
  const galleryInput = useRef<HTMLInputElement>(null);
  const cameraInput = useRef<HTMLInputElement>(null);
  const visibleItems = view.allow_multiple ? items : items.slice(-1);
  const successful = items.filter((item) => item.status === "complete").length;

  function receive(files: FileList | null) {
    if (!files?.length) return;
    onFiles(Array.from(files).slice(0, view.allow_multiple ? undefined : 1));
  }

  return (
    <article
      className="overflow-hidden rounded-xl border border-border bg-card shadow-[var(--shadow-sm)]"
      onDragOver={(event) => event.preventDefault()}
      onDrop={(event) => {
        event.preventDefault();
        if (!disabled) receive(event.dataTransfer.files);
      }}
    >
      <div className="grid gap-4 p-4 sm:grid-cols-[52px_minmax(0,1fr)_auto] sm:items-start sm:p-5">
        <div className="data-value flex h-11 w-11 items-center justify-center rounded-xl bg-navy text-sm font-bold text-white">
          {String(index + 1).padStart(2, "0")}
        </div>
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <h3 className="text-base font-bold tracking-tight">{localizedText(language, view.name)}</h3>
            <Badge variant={view.required ? "warning" : "neutral"}>{view.required ? c.required : c.optional}</Badge>
            <Badge variant="neutral">{view.role === "reference_front" ? c.reference : view.view_kind === "detail" ? c.detail : c.standard}</Badge>
            {view.allow_multiple ? <Badge variant="neutral">{c.multiple}</Badge> : null}
          </div>
          <p className="mt-2 max-w-3xl text-sm leading-6 text-muted-foreground">{localizedText(language, view.instruction)}</p>
          {view.reference_images.length ? (
            <div className="mt-4">
              <p className="mb-2 text-xs font-semibold text-muted-foreground">{c.referenceImages}</p>
              <div className="flex flex-wrap gap-2">
                {view.reference_images.map((reference, referenceIndex) => (
                  // Authenticated media is intentionally served through the app proxy.
                  // eslint-disable-next-line @next/next/no-img-element
                  <img
                    alt={`${c.referenceAlt} ${referenceIndex + 1}`}
                    className="h-16 w-16 rounded-lg border border-border bg-muted object-cover"
                    key={reference.public_id}
                    loading="lazy"
                    src={authenticatedMediaURL(reference.thumbnail_url)}
                  />
                ))}
              </div>
            </div>
          ) : null}
        </div>
        <div className="flex items-center gap-2 sm:justify-end">
          {successful ? <Check aria-label={c.complete} className="h-5 w-5 text-success" /> : <ImageIcon aria-hidden className="h-5 w-5 text-muted-foreground" />}
          <span className="text-sm font-semibold">{view.allow_multiple && successful ? successful : successful ? c.complete : "—"}</span>
        </div>
      </div>

      <div className="border-t border-border bg-muted/35 p-4 sm:p-5">
        <div className="grid gap-3 md:grid-cols-[minmax(0,1fr)_auto_auto] md:items-center">
          <button
            className="flex min-h-14 cursor-pointer items-center justify-center gap-2 rounded-lg border border-dashed border-primary/35 bg-card px-4 text-sm font-medium text-primary transition-colors hover:border-primary hover:bg-primary/5 disabled:cursor-not-allowed disabled:opacity-45"
            disabled={disabled}
            onClick={() => galleryInput.current?.click()}
            type="button"
          >
            <CloudUpload className="h-5 w-5" />
            <span>{c.drop}</span>
          </button>
          <Button disabled={disabled} onClick={() => galleryInput.current?.click()} variant="secondary">
            <ImageIcon className="h-4 w-4" /> {c.chooseFiles}
          </Button>
          <Button disabled={disabled} onClick={() => cameraInput.current?.click()} variant="secondary">
            <Camera className="h-4 w-4" /> {c.takePhoto}
          </Button>
          <input
            accept="image/*"
            aria-label={`${c.chooseFiles}: ${localizedText(language, view.name)}`}
            className="sr-only"
            disabled={disabled}
            multiple={view.allow_multiple}
            onChange={(event) => { receive(event.target.files); event.target.value = ""; }}
            ref={galleryInput}
            type="file"
          />
          <input
            accept="image/*"
            aria-label={`${c.takePhoto}: ${localizedText(language, view.name)}`}
            capture="environment"
            className="sr-only"
            disabled={disabled}
            onChange={(event) => { receive(event.target.files); event.target.value = ""; }}
            ref={cameraInput}
            type="file"
          />
        </div>
        <p className="mt-2 text-xs leading-5 text-muted-foreground">{c.idleHelp}</p>

        {visibleItems.length ? (
          <div className="mt-4 grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
            {visibleItems.map((item) => (
              <div className="flex min-w-0 gap-3 rounded-lg border border-border bg-card p-3" key={item.id}>
                {/* Local object URLs and authenticated media endpoints cannot use Next image optimization. */}
                {/* eslint-disable-next-line @next/next/no-img-element */}
                <img alt="" className="h-16 w-16 shrink-0 rounded-md bg-muted object-cover" src={item.previewURL} />
                <div className="min-w-0 flex-1">
                  <p className="truncate text-xs font-semibold">{item.file.name}</p>
                  <div className={cn("mt-1.5 flex items-center gap-1.5 text-xs", item.status === "error" ? "text-danger" : item.status === "complete" ? "text-success" : "text-muted-foreground")}>
                    {item.status === "optimizing" || item.status === "uploading" ? <LoaderCircle className="h-3.5 w-3.5 animate-spin" /> : null}
                    {item.status === "complete" ? <Check className="h-3.5 w-3.5" /> : null}
                    {item.status === "error" ? <CircleAlert className="h-3.5 w-3.5" /> : null}
                    <span>{item.status === "optimizing" ? c.optimizing : item.status === "uploading" ? c.uploading : item.status === "complete" ? c.complete : c.failed}</span>
                  </div>
                  {item.error ? <p className="mt-1 text-xs leading-5 text-danger">{item.error}</p> : null}
                  {item.status === "error" ? (
                    <button className="mt-2 inline-flex min-h-11 cursor-pointer items-center gap-1.5 text-xs font-semibold text-primary" disabled={disabled} onClick={() => onRetry(item)} type="button">
                      <RotateCcw className="h-3.5 w-3.5" /> {c.retry}
                    </button>
                  ) : null}
                </div>
              </div>
            ))}
          </div>
        ) : null}
      </div>
    </article>
  );
}

export default function CapturePage() {
  const { language } = useLanguage();
  const c = copy[language];
  const [selectedSKUID, setSelectedSKUID] = useState("");
  const [selectedVersionID, setSelectedVersionID] = useState("");
  const [session, setSession] = useState<PhotoSession>();
  const [uploads, setUploads] = useState<Record<string, UploadItem[]>>({});
  const [busy, setBusy] = useState(false);
  const [announcement, setAnnouncement] = useState("");
  const sessionRef = useRef<PhotoSession | undefined>(undefined);
  const previewURLs = useRef(new Set<string>());

  const skuQuery = useQuery({
    queryKey: ["capture", "skus"],
    queryFn: () => apiRequest<{ data: SKU[] }>("/skus"),
  });
  const activeSKUs = useMemo(() => (skuQuery.data?.data ?? []).filter((sku) => sku.status === "active"), [skuQuery.data]);
  const selectedSKU = activeSKUs.find((sku) => sku.public_id === selectedSKUID);
  const categoryID = selectedSKU?.product.category_id;
  const sopQuery = useQuery({
    queryKey: ["capture", "sops", categoryID],
    enabled: Boolean(categoryID),
    queryFn: () => apiRequest<{ data: SOPSummary[] }>(`/capture-sops?category_id=${categoryID}`),
  });
  const publishedVersions = useMemo(
    () => (sopQuery.data?.data ?? [])
      .filter((summary) => summary.category_id === categoryID)
      .flatMap((summary) => summary.versions)
      .filter((version) => version.status === "published")
      .sort((left, right) => versionTimestamp(right) - versionTimestamp(left) || right.version_number - left.version_number),
    [categoryID, sopQuery.data],
  );
  const effectiveVersionID = publishedVersions.some((version) => version.public_id === selectedVersionID)
    ? selectedVersionID
    : publishedVersions[0]?.public_id ?? "";
  const selectedVersion = publishedVersions.find((version) => version.public_id === effectiveVersionID);
  const views = useMemo(() => [...(selectedVersion?.views ?? [])].sort((left, right) => left.sequence - right.sequence), [selectedVersion]);
  const requiredViews = views.filter((view) => view.required);
  const completedRequired = requiredViews.filter((view) => (uploads[view.public_id] ?? []).some((item) => item.status === "complete")).length;
  const canFinish = Boolean(selectedVersion) && completedRequired === requiredViews.length;

  useEffect(() => {
    const urls = previewURLs.current;
    return () => urls.forEach((url) => URL.revokeObjectURL(url));
  }, []);

  function clearCaptureState() {
    previewURLs.current.forEach((url) => URL.revokeObjectURL(url));
    previewURLs.current.clear();
    setUploads({});
    setSession(undefined);
    sessionRef.current = undefined;
    setAnnouncement("");
  }

  function changeSKU(value: string) {
    clearCaptureState();
    setSelectedSKUID(value);
    setSelectedVersionID("");
  }

  function changeVersion(value: string) {
    clearCaptureState();
    setSelectedVersionID(value);
  }

  function updateItem(viewID: string, itemID: string, change: Partial<UploadItem>) {
    setUploads((current) => ({
      ...current,
      [viewID]: (current[viewID] ?? []).map((item) => item.id === itemID ? { ...item, ...change } : item),
    }));
  }

  async function ensureSession(): Promise<PhotoSession> {
    if (sessionRef.current) return sessionRef.current;
    if (!selectedSKU || !selectedVersion) throw new Error("capture selection missing");
    const created = await apiRequest<PhotoSession>("/photo-sessions", {
      method: "POST",
      body: JSON.stringify({ sku_id: selectedSKU.public_id, sop_version_id: selectedVersion.public_id }),
    });
    sessionRef.current = created;
    setSession(created);
    return created;
  }

  async function uploadOne(view: SOPView, item: UploadItem) {
    try {
      updateItem(view.public_id, item.id, { status: "optimizing", error: undefined });
      const normalized = await normalizeCaptureImage(item.file);
      updateItem(view.public_id, item.id, { status: "uploading" });
      const activeSession = await ensureSession();
      const upload = await apiRequest<UploadEnvelope>("/assets/upload-url", {
        method: "POST",
        body: JSON.stringify({
          file_name: normalized.file.name,
          content_type: normalized.file.type,
          photo_session_id: activeSession.public_id,
          sop_view_id: view.public_id,
        }),
      });
      const stored = await fetch(upload.upload_url, { method: upload.method, headers: upload.headers, body: normalized.file });
      if (!stored.ok) throw new Error("storage upload failed");
      const asset = await apiRequest<CompletedAsset>("/assets/complete", {
        method: "POST",
        body: JSON.stringify({
          photo_session_id: activeSession.public_id,
          sop_view_id: view.public_id,
          completion_token: upload.completion_token,
          captured_at: new Date(item.file.lastModified || Date.now()).toISOString(),
        }),
      });
      if (previewURLs.current.delete(item.previewURL)) URL.revokeObjectURL(item.previewURL);
      updateItem(view.public_id, item.id, { status: "complete", asset, previewURL: authenticatedMediaURL(asset.media_url) });
      setAnnouncement(c.uploaded);
    } catch (error) {
      const code = error instanceof CaptureImageError ? error.code : "upload_failed";
      updateItem(view.public_id, item.id, { status: "error", error: c[code] });
      setAnnouncement(c[code]);
    }
  }

  async function addFiles(view: SOPView, files: File[]) {
    if (!files.length || busy) return;
    const selectedFiles = view.allow_multiple ? files : files.slice(0, 1);
    const items = selectedFiles.map<UploadItem>((file) => {
      const previewURL = URL.createObjectURL(file);
      previewURLs.current.add(previewURL);
      return { id: uploadID(), file, previewURL, status: "optimizing" };
    });
    setUploads((current) => ({ ...current, [view.public_id]: [...(current[view.public_id] ?? []), ...items] }));
    setBusy(true);
    try {
      for (const item of items) await uploadOne(view, item);
    } finally {
      setBusy(false);
    }
  }

  async function retryUpload(view: SOPView, item: UploadItem) {
    if (busy) return;
    setBusy(true);
    try {
      await uploadOne(view, item);
    } finally {
      setBusy(false);
    }
  }

  const loading = skuQuery.isLoading || (Boolean(categoryID) && sopQuery.isLoading);
  const loadError = skuQuery.isError || sopQuery.isError;

  return (
    <div className="space-y-6">
      <div className="flex flex-col gap-4 lg:flex-row lg:items-end lg:justify-between">
        <div>
          <p className="mb-2 text-[11px] font-bold uppercase tracking-[0.16em] text-primary">{c.eyebrow}</p>
          <h1 className="text-3xl font-bold text-navy sm:text-4xl">{c.title}</h1>
          <p className="mt-2 max-w-2xl text-sm leading-6 text-muted-foreground">{c.description}</p>
        </div>
        {session ? (
          <div className="flex min-h-11 items-center gap-3 rounded-xl border border-border bg-card px-4 py-2 shadow-[var(--shadow-sm)]">
            <LockKeyhole className="h-4 w-4 text-primary" />
            <div>
              <p className="text-[10px] font-bold uppercase tracking-[0.14em] text-muted-foreground">{c.session}</p>
              <p className="data-value text-sm font-bold">{session.code}</p>
            </div>
          </div>
        ) : null}
      </div>

      <Card>
        <CardHeader><CardTitle>{c.setup}</CardTitle></CardHeader>
        <CardContent className="grid gap-4 md:grid-cols-2">
          <label className="grid gap-2 text-sm font-semibold" htmlFor="capture-sku">
            {c.selectSKU}
            <select
              className="h-11 w-full rounded-lg border border-border bg-card px-3.5 text-sm font-normal outline-none transition-colors hover:border-[#b7c8c5] focus:border-primary focus:ring-3 focus:ring-primary/10 disabled:cursor-not-allowed disabled:bg-muted/60"
              disabled={busy || Boolean(session)}
              id="capture-sku"
              onChange={(event) => changeSKU(event.target.value)}
              value={selectedSKUID}
            >
              <option value="">{c.chooseSKU}</option>
              {activeSKUs.map((sku) => <option key={sku.public_id} value={sku.public_id}>{sku.code} · {sku.product.name}</option>)}
            </select>
          </label>
          <label className="grid gap-2 text-sm font-semibold" htmlFor="capture-sop">
            {c.selectSOP}
            <select
              className="h-11 w-full rounded-lg border border-border bg-card px-3.5 text-sm font-normal outline-none transition-colors hover:border-[#b7c8c5] focus:border-primary focus:ring-3 focus:ring-primary/10 disabled:cursor-not-allowed disabled:bg-muted/60"
              disabled={!selectedSKU || busy || Boolean(session) || !publishedVersions.length}
              id="capture-sop"
              onChange={(event) => changeVersion(event.target.value)}
              value={effectiveVersionID}
            >
              <option value="">{c.chooseSOP}</option>
              {publishedVersions.map((version) => <option key={version.public_id} value={version.public_id}>{localizedText(language, version.name)} · v{version.version_number}</option>)}
            </select>
          </label>
          {session ? <p className="flex items-center gap-2 text-xs text-muted-foreground md:col-span-2"><LockKeyhole className="h-3.5 w-3.5" />{c.sessionLocked}</p> : null}
          {!skuQuery.isLoading && !activeSKUs.length ? <p className="text-sm text-muted-foreground md:col-span-2">{c.noSKU}</p> : null}
          {selectedSKU && !sopQuery.isLoading && !publishedVersions.length ? <p className="text-sm text-warning md:col-span-2">{c.noSOP}</p> : null}
        </CardContent>
      </Card>

      {loading ? <div className="flex min-h-32 items-center justify-center gap-2 rounded-xl border border-border bg-card text-sm text-muted-foreground"><LoaderCircle className="h-4 w-4 animate-spin" />{c.loading}</div> : null}
      {loadError ? <div className="rounded-xl border border-danger/25 bg-[#fff0ee] p-4 text-sm text-danger" role="alert">{c.loadError}</div> : null}

      {selectedVersion ? (
        <section aria-labelledby="shot-list-title" className="space-y-4">
          <div className="flex flex-col gap-3 rounded-xl bg-navy p-4 text-white shadow-[var(--shadow-md)] sm:flex-row sm:items-center sm:justify-between sm:p-5">
            <div>
              <p className="text-[10px] font-bold uppercase tracking-[0.18em] text-white/45">{c.shotList}</p>
              <h2 className="mt-1 text-lg font-bold" id="shot-list-title">{localizedText(language, selectedVersion.name)} · v{selectedVersion.version_number}</h2>
            </div>
            <div className="min-w-[220px]">
              <div className="flex items-center justify-between text-xs font-semibold">
                <span className="text-white/65">{c.requiredProgress}</span>
                <span className="data-value">{completedRequired}/{requiredViews.length}</span>
              </div>
              <div aria-label={`${c.requiredProgress}: ${completedRequired}/${requiredViews.length}`} aria-valuemax={requiredViews.length} aria-valuemin={0} aria-valuenow={completedRequired} className="mt-2 h-2 overflow-hidden rounded-full bg-white/12" role="progressbar">
                <div className="h-full rounded-full bg-[#51c58b] transition-[width] duration-200" style={{ width: `${requiredViews.length ? (completedRequired / requiredViews.length) * 100 : 100}%` }} />
              </div>
            </div>
          </div>

          {views.map((view, index) => (
            <CaptureViewCard
              disabled={busy}
              index={index}
              items={uploads[view.public_id] ?? []}
              key={view.public_id}
              language={language}
              onFiles={(files) => void addFiles(view, files)}
              onRetry={(item) => void retryUpload(view, item)}
              view={view}
            />
          ))}

          <div className="flex flex-col gap-3 rounded-xl border border-border bg-card p-4 shadow-[var(--shadow-sm)] sm:flex-row sm:items-center sm:justify-between">
            <p className="text-sm text-muted-foreground">{c.finishHelp}</p>
            <Button asChild={canFinish} disabled={!canFinish || busy}>
              {canFinish ? <Link href="/assets/review">{c.finish}<ChevronRight className="h-4 w-4" /></Link> : <span>{c.finish}<ChevronRight className="h-4 w-4" /></span>}
            </Button>
          </div>
        </section>
      ) : null}

      <p aria-live="polite" className="sr-only" role="status">{announcement}</p>
    </div>
  );
}
