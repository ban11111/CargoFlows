"use client";

import { useQueryClient } from "@tanstack/react-query";
import { Archive, CheckCircle2, ClipboardPlus, Copy, LoaderCircle, Plus, Send, TriangleAlert } from "lucide-react";
import { useEffect, useMemo, useState } from "react";

import { SOPViewEditor } from "@/components/sop/sop-view-editor";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { ApiError, apiRequest } from "@/lib/api";
import { useLanguage } from "@/lib/i18n";
import { sopVersionSchema, sopViewSchema } from "@/lib/schemas";
import { addableSOPPresetKeys, localizedText, type LocalizedText, type SOPPresetKey, type SOPVersion, type SOPView, type ValidationResponse } from "@/lib/sop";

interface SOPVersionEditorProps {
  initialVersion: SOPVersion;
  onVersionChange?: (version: SOPVersion) => void;
}

type ServerError = ValidationResponse["errors"][number];

const labels = {
  zh: {
    title: "商品拍摄视图", saveMeta: "保存版本信息", addView: "添加视图", publish: "发布版本", copy: "复制为新版本", archive: "归档版本",
    immutablePublished: "已发布版本不可修改", immutableArchived: "已归档版本不可修改", draft: "草稿", published: "已发布", archived: "已归档",
    nameZh: "SOP 中文名称", nameEn: "SOP English name", descriptionZh: "中文说明", descriptionEn: "English description",
    validationFailed: "请修正以下问题后再发布", validationPassed: "验证通过，正在发布版本。", requestFailed: "请求失败，请检查输入后重试。",
    dirtyNotice: "请先保存所有未保存的修改，再执行发布、排序或新增视图。",
    preset: { back: "背面", left: "左侧", bottom: "底部", right: "右侧", top: "顶部", detail_label: "标签细节", packaging_front: "包装正面" },
  },
  en: {
    title: "Product capture views", saveMeta: "Save version details", addView: "Add view", publish: "Publish version", copy: "Copy as new version", archive: "Archive version",
    immutablePublished: "Published versions cannot be changed", immutableArchived: "Archived versions cannot be changed", draft: "Draft", published: "Published", archived: "Archived",
    nameZh: "SOP Chinese name", nameEn: "SOP English name", descriptionZh: "Chinese description", descriptionEn: "English description",
    validationFailed: "Fix the following issues before publishing", validationPassed: "Validation passed. Publishing version.", requestFailed: "Request failed. Check the input and try again.",
    dirtyNotice: "Save every unsaved change before publishing, reordering, or adding views.",
    preset: { back: "Back", left: "Left", bottom: "Bottom", right: "Right", top: "Top", detail_label: "Label detail", packaging_front: "Packaging front" },
  },
} as const;

export function SOPVersionEditor({ initialVersion, onVersionChange }: SOPVersionEditorProps) {
  const { language } = useLanguage();
  const c = labels[language];
  const queryClient = useQueryClient();
  const [version, setVersion] = useState(initialVersion);
  const [errors, setErrors] = useState<ServerError[]>([]);
  const [clientErrors, setClientErrors] = useState<Array<{ path: string; message: string }>>([]);
  const [busy, setBusy] = useState<string>();
  const [metadataDirty, setMetadataDirty] = useState(false);
  const [dirtyViewIDs, setDirtyViewIDs] = useState<Set<string>>(() => new Set());
  const immutable = version.status !== "draft";
  const dirty = metadataDirty || dirtyViewIDs.size > 0;
  const aggregateLocked = dirty || Boolean(busy);

  useEffect(() => {
    if (!dirty) return;
    const protectUnsaved = (event: BeforeUnloadEvent) => {
      event.preventDefault();
      event.returnValue = "";
    };
    window.addEventListener("beforeunload", protectUnsaved);
    return () => window.removeEventListener("beforeunload", protectUnsaved);
  }, [dirty]);

  const allErrors = useMemo(() => [
    ...errors.map((error) => ({ path: error.path, message: localizedText(language, error.message) })),
    ...clientErrors,
  ], [clientErrors, errors, language]);

  function replaceVersion(next: SOPVersion) {
    setVersion(next);
    setMetadataDirty(false);
    setDirtyViewIDs(new Set());
    setErrors([]);
    setClientErrors([]);
    onVersionChange?.(next);
    void queryClient.invalidateQueries({ queryKey: ["capture-sops"] });
    queryClient.setQueryData(["sop-version", next.public_id], next);
    void queryClient.invalidateQueries({ queryKey: ["sop-version", next.public_id] });
  }

  function syncConfirmedVersion(next: SOPVersion) {
    queryClient.setQueryData(["sop-version", next.public_id], next);
    void queryClient.invalidateQueries({ queryKey: ["sop-version", next.public_id] });
    void queryClient.invalidateQueries({ queryKey: ["capture-sops"] });
  }

  function readServerErrors(error: unknown) {
    if (error instanceof ApiError) {
      try {
        const body = JSON.parse(error.message) as Partial<ValidationResponse>;
        if (Array.isArray(body.errors)) {
          setErrors(body.errors);
          return;
        }
      } catch { /* use the generic request error */ }
    }
    setClientErrors([{ path: "", message: c.requestFailed }]);
  }

  async function run(key: string, operation: () => Promise<void>) {
    setBusy(key);
    setClientErrors([]);
    try { await operation(); } catch (error) { readServerErrors(error); } finally { setBusy(undefined); }
  }

  function updateView(index: number, view: SOPView) {
    setVersion((current) => ({ ...current, views: current.views.map((value, valueIndex) => valueIndex === index ? view : value) }));
    setDirtyViewIDs((current) => new Set(current).add(view.public_id));
  }

  async function saveMetadata() {
    const parsed = sopVersionSchema.safeParse(version);
    const metadataIssues = parsed.success ? [] : parsed.error.issues.filter((issue) => issue.path[0] === "name" || issue.path[0] === "description");
    if (metadataIssues.length) {
      setClientErrors(metadataIssues.map((issue) => ({ path: issue.path.join("."), message: issue.message })));
      return;
    }
    await run("metadata", async () => {
      const confirmed = await apiRequest<SOPVersion>(`/sop-versions/${version.public_id}`, {
        method: "PATCH", body: JSON.stringify({ name: version.name, description: version.description }),
      });
      const merged = { ...version, name: confirmed.name, description: confirmed.description, updated_at: confirmed.updated_at };
      setVersion(merged);
      setMetadataDirty(false);
      if (dirtyViewIDs.size === 0) syncConfirmedVersion(merged);
    });
  }

  async function saveView(index: number) {
    const view = version.views[index];
    const parsed = sopViewSchema.safeParse(view);
    if (!parsed.success) {
      setClientErrors(parsed.error.issues.map((issue) => ({ path: `views[${index}].${issue.path.join(".")}`, message: issue.message })));
      return;
    }
    const { role, view_kind, name, instruction, required, pose, composition } = parsed.data;
    await run(`view-${view.public_id}`, async () => {
      const confirmed = await apiRequest<SOPVersion>(`/sop-versions/${version.public_id}/views/${view.public_id}`, {
        method: "PATCH", body: JSON.stringify({ role, view_kind, name, instruction, required, pose, composition }),
      });
      const confirmedView = confirmed.views.find((item) => item.public_id === view.public_id);
      if (!confirmedView) throw new Error("saved view missing from response");
      const merged = { ...version, updated_at: confirmed.updated_at, views: version.views.map((item) => item.public_id === view.public_id ? confirmedView : item) };
      const remaining = new Set(dirtyViewIDs);
      remaining.delete(view.public_id);
      setVersion(merged);
      setDirtyViewIDs(remaining);
      if (!metadataDirty && remaining.size === 0) syncConfirmedVersion(merged);
    });
  }

  async function addPreset(presetKey: SOPPresetKey) {
    await run("add", async () => replaceVersion(await apiRequest<SOPVersion>(`/sop-versions/${version.public_id}/views`, {
      method: "POST", body: JSON.stringify({ preset_key: presetKey }),
    })));
  }

  async function deleteView(view: SOPView) {
    if (!window.confirm(`${language === "zh" ? "删除" : "Delete"} ${localizedText(language, view.name)}?`)) return;
    await run(`view-${view.public_id}`, async () => {
      await apiRequest<void>(`/sop-versions/${version.public_id}/views/${view.public_id}`, { method: "DELETE" });
      replaceVersion({ ...version, views: version.views.filter((item) => item.public_id !== view.public_id).map((item, index) => ({ ...item, sequence: index + 1 })) });
    });
  }

  async function moveView(index: number, direction: -1 | 1) {
    const next = [...version.views];
    const destination = Math.max(1, Math.min(next.length - 1, index + direction));
    if (destination !== index) [next[index], next[destination]] = [next[destination], next[index]];
    const publicIDs = next.map((view) => view.public_id);
    await run("reorder", async () => replaceVersion(await apiRequest<SOPVersion>(`/sop-versions/${version.public_id}/view-order`, {
      method: "PUT", body: JSON.stringify({ public_ids: publicIDs }),
    })));
  }

  async function validateAndPublish() {
    const local = sopVersionSchema.safeParse(version);
    if (!local.success) {
      setClientErrors(local.error.issues.map((issue) => ({ path: issue.path.join("."), message: issue.message })));
      return;
    }
    await run("publish", async () => {
      const validation = await apiRequest<ValidationResponse>(`/sop-versions/${version.public_id}/validate`, { method: "POST", body: "{}" });
      if (validation.errors.length) { setErrors(validation.errors); return; }
      replaceVersion(await apiRequest<SOPVersion>(`/sop-versions/${version.public_id}/publish`, { method: "POST", body: "{}" }));
    });
  }

  async function copyVersion() {
    await run("copy", async () => replaceVersion(await apiRequest<SOPVersion>(`/capture-sops/${version.sop_public_id}/versions`, {
      method: "POST", body: JSON.stringify({ source_version_id: version.public_id }),
    })));
  }

  async function archiveVersion() {
    if (!window.confirm(language === "zh" ? "归档后该版本不能用于新的拍摄批次。继续？" : "Archived versions cannot be used for new sessions. Continue?")) return;
    await run("archive", async () => replaceVersion(await apiRequest<SOPVersion>(`/sop-versions/${version.public_id}/archive`, { method: "POST", body: "{}" })));
  }

  async function uploadReference(viewIndex: number, file: File, caption: LocalizedText) {
    const view = version.views[viewIndex];
    await run(`reference-${view.public_id}`, async () => {
      const upload = await apiRequest<{ method: "PUT"; upload_url: string; asset_url: string; object_key: string; headers: Record<string, string> }>(`/sop-versions/${version.public_id}/views/${view.public_id}/reference-images/upload-url`, {
        method: "POST", body: JSON.stringify({ file_name: file.name, content_type: file.type }),
      });
      const result = await fetch(upload.upload_url, { method: upload.method, headers: upload.headers, body: file });
      if (!result.ok) throw new Error("reference upload failed");
      const image = await apiRequest<SOPView["reference_images"][number]>(`/sop-versions/${version.public_id}/views/${view.public_id}/reference-images`, {
        method: "POST", body: JSON.stringify({ object_key: upload.object_key, thumbnail_url: upload.asset_url, caption }),
      });
      const next = { ...version, views: version.views.map((item, index) => index === viewIndex ? { ...view, reference_images: [...view.reference_images, image] } : item) };
      setVersion(next);
      syncConfirmedVersion(next);
    });
  }

  async function deleteReference(viewIndex: number, imageID: string) {
    if (!window.confirm(language === "zh" ? "删除这张参考图？" : "Delete this reference image?")) return;
    const view = version.views[viewIndex];
    await run(`reference-${view.public_id}`, async () => {
      await apiRequest<void>(`/sop-versions/${version.public_id}/views/${view.public_id}/reference-images/${imageID}`, { method: "DELETE" });
      const next = { ...version, views: version.views.map((item, index) => index === viewIndex ? { ...view, reference_images: view.reference_images.filter((image) => image.public_id !== imageID).map((image, imageIndex) => ({ ...image, sort_order: imageIndex + 1 })) } : item) };
      setVersion(next);
      syncConfirmedVersion(next);
    });
  }

  async function moveReference(viewIndex: number, imageID: string, direction: -1 | 1) {
    const view = version.views[viewIndex];
    const index = view.reference_images.findIndex((image) => image.public_id === imageID);
    const destination = index + direction;
    if (destination < 0 || destination >= view.reference_images.length) return;
    const images = [...view.reference_images];
    [images[index], images[destination]] = [images[destination], images[index]];
    await run(`reference-${view.public_id}`, async () => replaceVersion(await apiRequest<SOPVersion>(`/sop-versions/${version.public_id}/views/${view.public_id}/reference-image-order`, {
      method: "PUT", body: JSON.stringify({ public_ids: images.map((image) => image.public_id) }),
    })));
  }

  const statusLabel = c[version.status];

  return (
    <div className="space-y-4">
      <header className="flex flex-col gap-3 border-b border-border pb-4 xl:flex-row xl:items-center xl:justify-between">
        <div><div className="flex items-center gap-2"><h1 className="text-2xl font-semibold tracking-tight">{c.title}</h1><Badge variant={version.status === "draft" ? "warning" : version.status === "published" ? "success" : "neutral"}>{statusLabel}</Badge><span className="font-mono text-xs tabular-nums text-muted-foreground">V{version.version_number}</span></div><p className="mt-1 text-sm text-muted-foreground">pcs_object_v1 · {version.views.length} {language === "zh" ? "个拍摄视图" : "capture views"}</p></div>
        <div className="flex flex-wrap gap-2">
          <Button className="min-h-11" disabled={immutable || aggregateLocked} onClick={validateAndPublish}><Send className="h-4 w-4" />{busy === "publish" ? <LoaderCircle className="h-4 w-4 animate-spin motion-reduce:animate-none" /> : null}{c.publish}</Button>
          <Button className="min-h-11" disabled={version.status !== "published" || Boolean(busy)} onClick={copyVersion} variant="secondary"><Copy className="h-4 w-4" />{c.copy}</Button>
          <Button className="min-h-11" disabled={version.status !== "published" || Boolean(busy)} onClick={archiveVersion} variant="danger"><Archive className="h-4 w-4" />{c.archive}</Button>
        </div>
      </header>

      {immutable ? <div className={`rounded-md border px-4 py-3 text-sm ${version.status === "published" ? "border-primary/30 bg-primary/5 text-primary" : "border-border bg-muted text-foreground"}`} role="status">{version.status === "published" ? c.immutablePublished : c.immutableArchived}</div> : null}
      {dirty ? <div className="rounded-md border border-warning/30 bg-[#fff4df] px-4 py-3 text-sm text-warning" role="status">{c.dirtyNotice}</div> : null}

      {allErrors.length ? <div className="rounded-md border border-danger/30 bg-danger/5 p-4" role="alert"><p className="flex items-center gap-2 font-semibold text-danger"><TriangleAlert className="h-4 w-4" />{c.validationFailed}</p><ul className="mt-2 list-disc space-y-1 pl-5 text-sm">{allErrors.map((error, index) => <li key={`${error.path}-${index}`}><a className="underline decoration-danger/40 underline-offset-2" href={pathToHref(error.path, version)}>{error.message}<span className="ml-2 font-mono text-xs text-muted-foreground">{error.path}</span></a></li>)}</ul></div> : null}

      <Card><CardContent className="grid gap-4 p-4 md:grid-cols-2">
        <Field id="sop-name-zh" label={c.nameZh}><Input aria-label={c.nameZh} className="h-11" disabled={Boolean(busy)} id="sop-name-zh" onChange={(e) => { setMetadataDirty(true); setVersion({ ...version, name: { ...version.name, "zh-CN": e.target.value } }); }} readOnly={immutable} value={version.name["zh-CN"]} /></Field>
        <Field id="sop-name-en" label={c.nameEn}><Input aria-label={c.nameEn} className="h-11" disabled={Boolean(busy)} id="sop-name-en" onChange={(e) => { setMetadataDirty(true); setVersion({ ...version, name: { ...version.name, en: e.target.value } }); }} readOnly={immutable} value={version.name.en} /></Field>
        <Field id="sop-description-zh" label={c.descriptionZh}><Textarea disabled={Boolean(busy)} id="sop-description-zh" onChange={(e) => { setMetadataDirty(true); setVersion({ ...version, description: { ...version.description, "zh-CN": e.target.value } }); }} readOnly={immutable} value={version.description["zh-CN"]} /></Field>
        <Field id="sop-description-en" label={c.descriptionEn}><Textarea disabled={Boolean(busy)} id="sop-description-en" onChange={(e) => { setMetadataDirty(true); setVersion({ ...version, description: { ...version.description, en: e.target.value } }); }} readOnly={immutable} value={version.description.en} /></Field>
        <div className="flex justify-end md:col-span-2"><Button className="min-h-11" disabled={immutable || Boolean(busy) || !metadataDirty} onClick={saveMetadata} variant="secondary"><CheckCircle2 className="h-4 w-4" />{c.saveMeta}</Button></div>
      </CardContent></Card>

      <section aria-label={language === "zh" ? "拍摄顺序视图轨" : "Capture sequence view rail"} className="grid gap-2 rounded-lg border border-border bg-card p-3 sm:grid-cols-2 lg:grid-cols-4 xl:grid-cols-6">
        {version.views.map((view) => <a className="flex min-h-11 min-w-0 items-center gap-2 rounded-md border border-border px-2 text-sm outline-none hover:border-primary focus-visible:ring-2 focus-visible:ring-primary" href={`#view-${view.public_id}`} key={view.public_id}><span className="font-mono font-semibold tabular-nums">{String(view.sequence).padStart(2, "0")}</span><span className="truncate">{localizedText(language, view.name)}</span><span className="ml-auto text-xs text-muted-foreground">{view.role === "reference_front" ? language === "zh" ? "锁定" : "Locked" : view.required ? language === "zh" ? "必拍" : "Required" : language === "zh" ? "选拍" : "Optional"}</span></a>)}
      </section>

      <section className="rounded-lg border border-border bg-card p-3"><div className="flex flex-wrap items-center gap-2"><span className="mr-2 flex items-center gap-2 text-sm font-semibold"><ClipboardPlus className="h-4 w-4 text-primary" />{c.addView}</span>{addableSOPPresetKeys.map((preset) => <Button aria-label={`${language === "zh" ? "添加" : "Add "}${c.preset[preset]}`} className="min-h-11" disabled={immutable || aggregateLocked} key={preset} onClick={() => addPreset(preset)} size="sm" variant="secondary"><Plus className="h-3.5 w-3.5" />{c.preset[preset]}</Button>)}<Button aria-label={c.addView} className="sr-only" disabled={immutable || aggregateLocked} onClick={() => addPreset("detail_label")}>{c.addView}</Button></div></section>

      <div className="grid gap-4">
        {version.views.map((view, index) => <SOPViewEditor aggregateLocked={aggregateLocked} busy={Boolean(busy)} errorPaths={new Set(allErrors.filter((error) => error.path.startsWith(`views[${index}]`)).map((error) => error.path))} immutable={immutable} key={view.public_id} language={language} locked={view.role === "reference_front"} moveDownDisabled={view.role === "reference_front" || index === version.views.length - 1} moveUpDisabled={view.role === "reference_front" || index <= 1} onChange={(next) => updateView(index, next)} onDelete={() => deleteView(view)} onMove={(direction) => moveView(index, direction)} onReferenceDelete={(imageID) => deleteReference(index, imageID)} onReferenceMove={(imageID, direction) => moveReference(index, imageID, direction)} onReferenceUpload={(file, caption) => uploadReference(index, file, caption)} onSave={() => saveView(index)} saveDisabled={!dirtyViewIDs.has(view.public_id)} view={view} />)}
      </div>
    </div>
  );
}

function Field({ id, label, children }: { id: string; label: string; children: React.ReactNode }) { return <div className="space-y-1.5"><Label htmlFor={id}>{label}</Label>{children}</div>; }

function pathToHref(path: string, version: SOPVersion) {
  const match = /^views\[(\d+)]/.exec(path);
  const view = match ? version.views[Number(match[1])] : undefined;
  return view ? `#view-${view.public_id}` : "#sop-name-zh";
}
