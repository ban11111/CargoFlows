"use client";

import { useMutation, useQuery } from "@tanstack/react-query";
import { ArrowDown, ArrowLeft, ArrowUp, CheckCircle2, ImageIcon, LoaderCircle, Plus, Search, Send, Trash2, Type } from "lucide-react";
import Link from "next/link";
import { FormEvent, Suspense, useEffect, useRef, useState } from "react";
import { useSearchParams } from "next/navigation";

import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { aiTemplateDraftSchema, type AITemplateSlotInput } from "@/lib/ai-schemas";
import { ApiError, apiRequest } from "@/lib/api";
import { imageStyleKeys, imageStylePresets } from "@/lib/image-styles";
import { useLanguage } from "@/lib/i18n";
import type { components } from "@/lib/openapi-types";

type Template = components["schemas"]["AIContentTemplate"];
type Version = components["schemas"]["AIContentTemplateVersion"];
type Validation = components["schemas"]["AITemplateValidationResponse"];
type SlotKind = AITemplateSlotInput["kind"];
type EditableSlot = AITemplateSlotInput & { client_id: string; source?: components["schemas"]["AIContentSlot"] };
type ValidationRequest = { payload: components["schemas"]["AIContentTemplateMutationRequest"]; fingerprint: string };

export default function NewAITemplatePage() {
  return <Suspense fallback={<div className="h-72 animate-pulse rounded-lg bg-muted" />}><NewAITemplateEditor /></Suspense>;
}

function NewAITemplateEditor() {
  const { language } = useLanguage();
  const searchParams = useSearchParams();
  const templateID = searchParams?.get("template_id") ?? null;
  const versionID = searchParams?.get("version_id") ?? null;
  const loadedVersion = useRef("");
  const zh = language === "zh";
  const text = zh ? zhText : enText;
  const [nameZh, setNameZh] = useState("");
  const [nameEn, setNameEn] = useState("");
  const [platform, setPlatform] = useState("lazada");
  const [platformPrompt, setPlatformPrompt] = useState("");
  const [slots, setSlots] = useState<EditableSlot[]>([]);
  const [errorCodes, setErrorCodes] = useState<Record<string, string>>({});
  const [version, setVersion] = useState<Version | null>(null);
  const [validation, setValidation] = useState<Validation | null>(null);
  const [validatedFingerprint, setValidatedFingerprint] = useState("");
  const [blockedFingerprint, setBlockedFingerprint] = useState("");
  const existing = useQuery({ queryKey: ["ai-content-template", templateID], queryFn: () => apiRequest<Template>(`/ai-content-templates/${encodeURIComponent(templateID ?? "")}`), enabled: Boolean(templateID && versionID), retry: false });
  const errors = Object.fromEntries(Object.entries(errorCodes).map(([path, code]) => [path, localError(code, zh)]));
  const fingerprint = JSON.stringify(mutationPayload(nameZh, nameEn, platform, platformPrompt, slots));
  const currentValidation = validatedFingerprint === fingerprint ? validation : null;
  const clientValidationBlocked = blockedFingerprint === fingerprint && Object.keys(errorCodes).length > 0;

  useEffect(() => {
    const loaded = existing.data?.versions.find((candidate) => candidate.public_id === versionID);
    if (!loaded || loadedVersion.current === loaded.public_id) return;
    loadedVersion.current = loaded.public_id;
    setNameZh(existing.data?.name_zh ?? "");
    setNameEn(existing.data?.name_en ?? "");
    setPlatform(existing.data?.target_platform ?? "lazada");
    setPlatformPrompt(loaded.platform_prompt);
    setSlots(loaded.slots.map(editableSlotFromVersion));
    setVersion(loaded);
    setValidation(null);
  }, [existing.data, versionID]);

  const create = useMutation({
    mutationFn: (payload: components["schemas"]["AIContentTemplateMutationRequest"]) => apiRequest<Template>("/ai-content-templates", { method: "POST", body: JSON.stringify(payload) }),
    onSuccess(template) { setVersion(template.versions[0] ?? null); setValidation(null); },
  });
  const validate = useMutation({
    mutationFn: async ({ payload }: ValidationRequest) => {
      const updated = await apiRequest<Version>(`/ai-content-template-versions/${version?.public_id}`, { method: "PATCH", body: JSON.stringify(payload) });
      const response = await apiRequest<Validation>(`/ai-content-template-versions/${updated.public_id}/validate`, { method: "POST" });
      return { updated, response };
    },
    onSuccess(result, request) { setVersion(result.updated); setValidation(result.response); setValidatedFingerprint(request.fingerprint); },
  });
  const publish = useMutation({
    mutationFn: () => apiRequest<Version>(`/ai-content-template-versions/${version?.public_id}/publish`, { method: "POST" }),
    onSuccess(next) { setVersion(next); },
    onError(error) {
      const response = parseValidationError(error);
      if (response) { setValidation(response); setValidatedFingerprint(fingerprint); }
    },
  });
  const editorBusy = validate.isPending || publish.isPending;

  function submit(event: FormEvent) {
    event.preventDefault();
    const parsed = aiTemplateDraftSchema.safeParse({ name_zh: nameZh, name_en: nameEn, target_platform: platform, slots });
    const nextErrors: Record<string, string> = {};
    if (!parsed.success) parsed.error.issues.forEach((issue) => { nextErrors[issue.path.join(".")] ??= issue.message; });
    if (!platformPrompt.trim()) nextErrors.platform_prompt = "requiredPlatformPrompt";
    setErrorCodes(nextErrors);
    if (Object.keys(nextErrors).length || !parsed.success) return;
    create.mutate(mutationPayload(parsed.data.name_zh, parsed.data.name_en, parsed.data.target_platform, platformPrompt, parsed.data.slots));
  }

  function validateDraft() {
    const parsed = aiTemplateDraftSchema.safeParse({ name_zh: nameZh, name_en: nameEn, target_platform: platform, slots });
    const nextErrors: Record<string, string> = {};
    if (!parsed.success) parsed.error.issues.forEach((issue) => { nextErrors[issue.path.join(".")] ??= issue.message; });
    if (!platformPrompt.trim()) nextErrors.platform_prompt = "requiredPlatformPrompt";
    setErrorCodes(nextErrors);
    if (Object.keys(nextErrors).length || !parsed.success) {
      setBlockedFingerprint(fingerprint);
      return;
    }
    setBlockedFingerprint("");
    const payload = mutationPayload(parsed.data.name_zh, parsed.data.name_en, parsed.data.target_platform, platformPrompt, slots);
    validate.mutate({ payload, fingerprint: JSON.stringify(payload) });
  }

  function addSlot(kind: SlotKind) { setSlots((current) => [...current, defaultSlot(kind, nextSlotNumber(kind, current))]); }
  function updateSlot(index: number, patch: Partial<AITemplateSlotInput>) { setSlots((current) => current.map((slot, slotIndex) => slotIndex === index ? ({ ...slot, ...patch } as EditableSlot) : slot)); }
  function move(index: number, direction: -1 | 1) { const target = index + direction; if (target < 0 || target >= slots.length) return; setSlots((current) => { const next = [...current]; [next[index], next[target]] = [next[target], next[index]]; return next; }); setValidation(null); }

  if (existing.isLoading) return <div className="h-72 animate-pulse rounded-lg bg-muted" />;
  if (existing.isError) return <div className="rounded-lg border border-danger/30 bg-danger/5 p-5 text-sm text-danger" role="alert">{zh ? "无法载入模板草稿。" : "Could not load the template draft."}</div>;
  return <div className="mx-auto max-w-5xl space-y-6"><header><Button asChild className="mb-3 min-h-11" variant="ghost"><Link href="/ai-templates"><ArrowLeft className="h-4 w-4" />{text.back}</Link></Button><p className="text-xs font-semibold uppercase tracking-[0.14em] text-primary">CargoFlows · AI</p><h1 className="mt-2 text-2xl font-semibold tracking-tight">{versionID ? (zh ? "编辑 AI 内容模板" : "Edit AI content template") : text.title}</h1><p className="mt-1 max-w-2xl text-sm leading-6 text-muted-foreground">{text.description}</p></header><form aria-busy={editorBusy} className="space-y-6" inert={editorBusy} onSubmit={submit}><Card><CardHeader><CardTitle>{text.basics}</CardTitle></CardHeader><CardContent className="grid gap-4 md:grid-cols-2"><Field error={errors.name_zh} id="template-name-zh" label={text.nameZh}><Input className="h-11" id="template-name-zh" maxLength={180} onChange={(event) => setNameZh(event.target.value)} value={nameZh} /></Field><Field error={errors.name_en} id="template-name-en" label={text.nameEn}><Input className="h-11" id="template-name-en" maxLength={180} onChange={(event) => setNameEn(event.target.value)} value={nameEn} /></Field><Field error={errors.target_platform} id="template-platform" label={text.platform}><Input className="h-11" id="template-platform" maxLength={80} onChange={(event) => setPlatform(event.target.value)} value={platform} /></Field><div className="space-y-1.5 md:col-span-2"><Label htmlFor="platform-prompt">{text.platformPrompt}</Label><Textarea id="platform-prompt" onChange={(event) => setPlatformPrompt(event.target.value)} placeholder={text.platformPromptPlaceholder} value={platformPrompt} />{errors.platform_prompt ? <p className="text-sm text-danger" role="alert">{errors.platform_prompt}</p> : null}</div></CardContent></Card><Card><CardHeader><div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between"><div><CardTitle>{text.slots}</CardTitle><p className="mt-1 text-sm text-muted-foreground">{text.slotsHelp}</p></div><div className="flex flex-wrap gap-2"><AddButton icon={Type} label={text.addTitle} onClick={() => addSlot("title")} /><AddButton icon={Search} label={text.addSeo} onClick={() => addSlot("seo_description")} /><AddButton icon={ImageIcon} label={text.addImage} onClick={() => addSlot("image")} /></div></div></CardHeader><CardContent className="space-y-4">{slots.length === 0 ? <div className="rounded-lg border border-dashed border-border p-8 text-center text-sm text-muted-foreground">{text.noSlots}</div> : null}{slots.map((slot, index) => <SlotEditor errors={errors} index={index} key={slot.client_id} onMove={move} onRemove={() => setSlots((current) => current.filter((_, itemIndex) => itemIndex !== index))} onUpdate={updateSlot} slot={slot} text={text} />)}{errors.slots ? <p className="text-sm text-danger" role="alert">{errors.slots}</p> : null}</CardContent></Card>{create.isError ? <p className="rounded-md border border-danger/30 bg-danger/5 p-3 text-sm text-danger" role="alert">{text.createError}</p> : null}{!version ? <div className="flex justify-end"><Button className="min-h-11" disabled={create.isPending} type="submit">{create.isPending ? <LoaderCircle className="h-4 w-4 animate-spin motion-reduce:animate-none" /> : <Plus className="h-4 w-4" />}{text.create}</Button></div> : null}</form>{version ? <Card><CardHeader><CardTitle>{text.publication}</CardTitle></CardHeader><CardContent className="space-y-4"><div className="flex flex-wrap items-center gap-3"><span className="rounded-full border border-warning/30 bg-warning/5 px-3 py-1 text-xs font-semibold text-warning">V{version.version_number} · {version.status}</span><Button className="min-h-11" disabled={editorBusy || version.status !== "draft"} onClick={validateDraft} type="button" variant="secondary">{validate.isPending ? <LoaderCircle className="h-4 w-4 animate-spin motion-reduce:animate-none" /> : <CheckCircle2 className="h-4 w-4" />}{versionID ? (zh ? "保存并校验" : "Save and validate") : text.validate}</Button><Button className="min-h-11" disabled={editorBusy || currentValidation?.code !== "template_valid" || version.status !== "draft"} onClick={() => publish.mutate()} type="button"><Send className="h-4 w-4" />{version.status === "published" ? text.published : text.publish}</Button></div>{clientValidationBlocked ? <ClientIssueList errors={errors} slots={slots} text={text} zh={zh} /> : null}{currentValidation ? currentValidation.code === "template_valid" ? <p className="text-sm text-success" role="status">{text.valid}</p> : <IssueList issues={currentValidation.issues} title={text.validationIssues} /> : <p className="text-sm text-muted-foreground">{text.validateFirst}</p>}{validate.isError || publish.isError ? <p className="text-sm text-danger" role="alert">{text.actionError}</p> : null}</CardContent></Card> : null}</div>;
}

function editableSlotFromVersion(slot: components["schemas"]["AIContentSlot"]): EditableSlot {
  const constraints = asObject(slot.constraints);
  const generation = asObject(slot.generation_config);
  if (slot.kind === "image") {
    const configuredStyles = stringList(generation.allowed_styles);
    const configuredDefault = stringValue(generation.style, "");
    const allowedStyles = configuredStyles.length ? configuredStyles : configuredDefault ? [configuredDefault] : [...imageStyleKeys];
    return { client_id: crypto.randomUUID(), source: slot, kind: slot.kind, slot_key: slot.slot_key, name_zh: slot.name_zh, name_en: slot.name_en, prompt_fragment: slot.prompt_fragment, size: stringValue(generation.size, "1024x1024") as "1024x1024", quality: stringValue(generation.quality, "high") as "high", candidate_count: numberValue(generation.candidate_count, 1), style: allowedStyles.includes(configuredDefault) ? configuredDefault : allowedStyles[0], allowed_styles: allowedStyles };
  }
  if (slot.kind === "title") return { client_id: crypto.randomUUID(), source: slot, kind: slot.kind, slot_key: slot.slot_key, name_zh: slot.name_zh, name_en: slot.name_en, prompt_fragment: slot.prompt_fragment, min_length: numberValue(constraints.min_length, 10), max_length: numberValue(constraints.max_length, 120) };
  return { client_id: crypto.randomUUID(), source: slot, kind: slot.kind, slot_key: slot.slot_key, name_zh: slot.name_zh, name_en: slot.name_en, prompt_fragment: slot.prompt_fragment, max_length: numberValue(constraints.max_length, 800) };
}

function asObject(value: unknown): Record<string, unknown> { return value && typeof value === "object" && !Array.isArray(value) ? { ...(value as Record<string, unknown>) } : {}; }
function stringValue(value: unknown, fallback: string) { return typeof value === "string" ? value : fallback; }
function numberValue(value: unknown, fallback: number) { return typeof value === "number" && Number.isFinite(value) ? value : fallback; }
function stringList(value: unknown) { return Array.isArray(value) ? value.filter((item): item is string => typeof item === "string") : []; }

function SlotEditor({ slot, index, onUpdate, onMove, onRemove, errors, text }: { slot: AITemplateSlotInput; index: number; onUpdate: (index: number, patch: Partial<AITemplateSlotInput>) => void; onMove: (index: number, direction: -1 | 1) => void; onRemove: () => void; errors: Record<string, string>; text: typeof zhText }) {
  const prefix = `slots.${index}`;
  const { language } = useLanguage();
  return <section className="rounded-lg border border-border bg-muted/20 p-4" aria-label={`${text.slot} ${index + 1}`}>
    <div className="mb-4 flex items-center justify-between gap-3"><div><strong>{index + 1}. {text.kinds[slot.kind]}</strong><p className="text-xs text-muted-foreground">{slot.kind}</p></div><div className="flex gap-1"><IconButton label={text.moveUp} onClick={() => onMove(index, -1)}><ArrowUp className="h-4 w-4" /></IconButton><IconButton label={text.moveDown} onClick={() => onMove(index, 1)}><ArrowDown className="h-4 w-4" /></IconButton><IconButton label={text.remove} onClick={onRemove}><Trash2 className="h-4 w-4" /></IconButton></div></div>
    <div className="grid gap-4 md:grid-cols-2">
      <Field error={errors[`${prefix}.slot_key`]} id={`${prefix}-key`} label={text.slotKey}><Input id={`${prefix}-key`} maxLength={80} onChange={(event) => onUpdate(index, { slot_key: event.target.value })} value={slot.slot_key} /></Field>
      <Field error={errors[`${prefix}.name_zh`]} id={`${prefix}-zh`} label={text.slotNameZh}><Input id={`${prefix}-zh`} maxLength={180} onChange={(event) => onUpdate(index, { name_zh: event.target.value })} value={slot.name_zh} /></Field>
      <Field error={errors[`${prefix}.name_en`]} id={`${prefix}-en`} label={text.slotNameEn}><Input id={`${prefix}-en`} maxLength={180} onChange={(event) => onUpdate(index, { name_en: event.target.value })} value={slot.name_en} /></Field>
      <div className="space-y-1.5 md:col-span-2"><Label htmlFor={`${prefix}-prompt`}>{text.prompt}</Label><Textarea id={`${prefix}-prompt`} onChange={(event) => onUpdate(index, { prompt_fragment: event.target.value })} value={slot.prompt_fragment} />{errors[`${prefix}.prompt_fragment`] ? <p className="text-sm text-danger">{errors[`${prefix}.prompt_fragment`]}</p> : null}</div>
      {slot.kind === "image" ? <>
        <Field id={`${prefix}-size`} label={text.size}><select className="h-11 w-full rounded-md border border-border bg-card px-3" id={`${prefix}-size`} onChange={(event) => onUpdate(index, { size: event.target.value as "1024x1024" })} value={slot.size}><option>1024x1024</option><option>1536x1024</option><option>1024x1536</option></select></Field>
        <Field id={`${prefix}-quality`} label={text.quality}><select className="h-11 w-full rounded-md border border-border bg-card px-3" id={`${prefix}-quality`} onChange={(event) => onUpdate(index, { quality: event.target.value as "high" })} value={slot.quality}><option value="low">low</option><option value="medium">medium</option><option value="high">high</option></select></Field>
        <Field id={`${prefix}-count`} label={text.candidates}><Input id={`${prefix}-count`} max={4} min={1} onChange={(event) => onUpdate(index, { candidate_count: Number(event.target.value) })} inputMode="numeric" type="number" value={slot.candidate_count} /></Field>
        <div className="space-y-3 md:col-span-2">
          <div className="flex flex-wrap items-center justify-between gap-2"><div><p className="text-sm font-medium">{language === "zh" ? "允许的图片风格" : "Allowed image styles"}</p><p className="text-xs text-muted-foreground">{language === "zh" ? `已启用 ${slot.allowed_styles.length}/20；任务创建时选择一种。` : `${slot.allowed_styles.length}/20 enabled; choose one per job.`}</p></div><div className="flex gap-2"><Button className="min-h-11" onClick={() => onUpdate(index, { allowed_styles: [...imageStyleKeys], style: imageStyleKeys.includes(slot.style) ? slot.style : imageStyleKeys[0] })} type="button" variant="secondary">{language === "zh" ? "全选" : "Select all"}</Button><Button className="min-h-11" onClick={() => onUpdate(index, { allowed_styles: [] })} type="button" variant="ghost">{language === "zh" ? "清空" : "Clear"}</Button></div></div>
          <fieldset className="grid gap-2 sm:grid-cols-2"><legend className="sr-only">{language === "zh" ? "允许的图片风格" : "Allowed image styles"}</legend>{imageStylePresets.map((preset) => { const checked = slot.allowed_styles.includes(preset.key); return <label className={`flex min-h-14 cursor-pointer items-start gap-3 rounded-md border p-3 ${checked ? "border-primary bg-primary/5" : "border-border bg-card"}`} key={preset.key}><input checked={checked} className="mt-1 h-5 w-5 accent-[var(--color-primary)]" onChange={() => { const allowed = checked ? slot.allowed_styles.filter((key) => key !== preset.key) : [...slot.allowed_styles, preset.key]; onUpdate(index, { allowed_styles: allowed, style: allowed.includes(slot.style) ? slot.style : (allowed[0] ?? slot.style) }); }} type="checkbox" /><span><span className="block text-sm font-medium">{preset.name[language]}</span><span className="mt-1 block text-xs leading-5 text-muted-foreground">{preset.description[language]}</span></span></label>; })}</fieldset>
          {errors[`${prefix}.allowed_styles`] ? <p className="text-sm text-danger" role="alert">{errors[`${prefix}.allowed_styles`]}</p> : null}
          <Field id={`${prefix}-style`} label={language === "zh" ? "默认图片风格" : "Default image style"}><select className="h-11 w-full rounded-md border border-border bg-card px-3" id={`${prefix}-style`} onChange={(event) => onUpdate(index, { style: event.target.value })} value={slot.style}>{slot.allowed_styles.map((key) => { const preset = imageStylePresets.find((item) => item.key === key); return <option key={key} value={key}>{preset?.name[language] ?? `${key} · ${language === "zh" ? "旧版/自定义" : "Legacy/custom"}`}</option>; })}</select></Field>
        </div>
      </> : null}
      {slot.kind === "title" ? <><Field id={`${prefix}-min`} label={text.minLength}><Input id={`${prefix}-min`} min={1} onChange={(event) => onUpdate(index, { min_length: Number(event.target.value) })} inputMode="numeric" type="number" value={slot.min_length} /></Field><Field id={`${prefix}-max`} label={text.maxLength}><Input id={`${prefix}-max`} max={500} min={1} onChange={(event) => onUpdate(index, { max_length: Number(event.target.value) })} inputMode="numeric" type="number" value={slot.max_length} /></Field></> : null}
      {slot.kind === "seo_description" ? <Field id={`${prefix}-seo-max`} label={text.maxLength}><Input id={`${prefix}-seo-max`} max={10000} min={1} onChange={(event) => onUpdate(index, { max_length: Number(event.target.value) })} inputMode="numeric" type="number" value={slot.max_length} /></Field> : null}
    </div>
  </section>;
}

function Field({ id, label, error, children }: { id: string; label: string; error?: string; children: React.ReactNode }) { return <div className="space-y-1.5"><Label htmlFor={id}>{label}</Label>{children}{error ? <p className="text-sm text-danger" role="alert">{error}</p> : null}</div>; }
function AddButton({ icon: Icon, label, onClick }: { icon: typeof Plus; label: string; onClick: () => void }) { return <Button className="min-h-11" onClick={onClick} type="button" variant="secondary"><Icon className="h-4 w-4" />{label}</Button>; }
function IconButton({ label, onClick, children }: { label: string; onClick: () => void; children: React.ReactNode }) { return <Button aria-label={label} className="min-h-11 min-w-11" onClick={onClick} size="icon" type="button" variant="ghost">{children}</Button>; }
function ClientIssueList({ errors, slots, text, zh }: { errors: Record<string, string>; slots: EditableSlot[]; text: typeof zhText; zh: boolean }) { return <div className="rounded-md border border-danger/30 bg-danger/5 p-3 text-sm text-danger" role="alert"><p className="font-medium">{text.fixFormErrors}</p><ul className="mt-2 list-disc space-y-1 pl-5">{Object.entries(errors).map(([path, message]) => <li key={path}><strong>{clientFieldLabel(path, slots, text, zh)}</strong>：{message}</li>)}</ul></div>; }
function IssueList({ issues, title }: { issues: Validation["issues"]; title: string }) { const { language } = useLanguage(); return <div className="rounded-lg border border-danger/30 bg-danger/5 p-4" role="alert"><p className="font-medium text-danger">{title}</p><ul className="mt-2 space-y-1 text-sm text-danger">{issues.map((issue, index) => <li key={`${issue.path}-${index}`}><code>{issue.path}</code>: {serverIssueText(issue.code, issue.message, language === "zh")}</li>)}</ul></div>; }

function clientFieldLabel(path: string, slots: EditableSlot[], text: typeof zhText, zh: boolean) {
  const rootLabels: Record<string, string> = { name_zh: text.nameZh, name_en: text.nameEn, target_platform: text.platform, platform_prompt: text.platformPrompt, slots: text.slots };
  if (rootLabels[path]) return rootLabels[path];
  const match = /^slots\.(\d+)\.(.+)$/.exec(path);
  if (!match) return path;
  const index = Number(match[1]);
  const field = match[2];
  const fieldLabels: Record<string, string> = { slot_key: text.slotKey, name_zh: text.slotNameZh, name_en: text.slotNameEn, prompt_fragment: text.prompt, size: text.size, quality: text.quality, candidate_count: text.candidates, min_length: text.minLength, max_length: text.maxLength, allowed_styles: zh ? "允许的图片风格" : "Allowed image styles", style: zh ? "默认图片风格" : "Default image style" };
  const slotName = slots[index] ? (zh ? slots[index].name_zh : slots[index].name_en) : "";
  return `${text.slot} ${index + 1}${slotName ? `（${slotName}）` : ""} · ${fieldLabels[field] ?? field}`;
}

function defaultSlot(kind: SlotKind, number: number): EditableSlot {
  const base = { client_id: crypto.randomUUID(), slot_key: `${kind === "seo_description" ? "seo" : kind}_${number}`, name_zh: "", name_en: "", prompt_fragment: "" };
  if (kind === "image") return { ...base, kind, size: "1024x1024", quality: "high", candidate_count: 1, style: imageStyleKeys[0], allowed_styles: [...imageStyleKeys] };
  if (kind === "title") return { ...base, kind, min_length: 10, max_length: 120 };
  return { ...base, kind, max_length: 800 };
}

function nextSlotNumber(kind: SlotKind, slots: AITemplateSlotInput[]) {
  const prefix = kind === "seo_description" ? "seo" : kind;
  const used = new Set(slots.map((slot) => slot.slot_key));
  let number = 1;
  while (used.has(`${prefix}_${number}`)) number += 1;
  return number;
}

function mutationPayload(nameZh: string, nameEn: string, platform: string, platformPrompt: string, slots: AITemplateSlotInput[]): components["schemas"]["AIContentTemplateMutationRequest"] {
  return { name_zh: nameZh.trim(), name_en: nameEn.trim(), target_platform: platform.trim(), default_locale: "zh-CN", prompt_compiler_version: "v1", platform_prompt: platformPrompt.trim(), slots: slots.map((slot, sequence) => toMutationSlot(slot, sequence)) };
}

function parseValidationError(error: unknown): Validation | null {
  if (!(error instanceof ApiError) || error.status !== 422) return null;
  try {
    const parsed = JSON.parse(error.message) as Validation;
    return parsed.code === "template_validation_failed" && Array.isArray(parsed.issues) ? parsed : null;
  } catch {
    return null;
  }
}

function toMutationSlot(slot: AITemplateSlotInput, sequence: number): components["schemas"]["AIContentSlotMutation"] {
  const editable = slot as EditableSlot;
  const source = editable.source;
  const base = { slot_key: slot.slot_key, kind: slot.kind, name_zh: slot.name_zh, name_en: slot.name_en, description_zh: source?.description_zh ?? "", description_en: source?.description_en ?? "", sequence: sequence + 1, optional: source?.optional ?? true, default_selected: source?.default_selected ?? false, prompt_fragment: slot.prompt_fragment, layout_config: source?.layout_config ?? {} };
  if (slot.kind === "image") return { ...base, constraints: source?.constraints ?? {}, generation_config: { ...asObject(source?.generation_config), size: slot.size, quality: slot.quality, candidate_count: slot.candidate_count, style: slot.style, allowed_styles: slot.allowed_styles, allowed_candidate_count: [1, 2, 3, 4], allowed_sizes: ["1024x1024", "1536x1024", "1024x1536"], allowed_qualities: ["low", "medium", "high"], allow_user_extra_prompt: true } };
  if (slot.kind === "title") return { ...base, constraints: { ...asObject(source?.constraints), min_length: slot.min_length, max_length: slot.max_length }, generation_config: source?.generation_config ?? {} };
  return { ...base, constraints: { ...asObject(source?.constraints), max_length: slot.max_length }, generation_config: source?.generation_config ?? {} };
}

function localError(code: string, zh: boolean) { const map: Record<string, [string, string]> = { requiredNameZh: ["请输入中文名称", "Enter a Chinese name"], requiredNameEn: ["请输入英文名称", "Enter an English name"], requiredPlatform: ["请输入目标平台", "Enter a target platform"], requiredPlatformPrompt: ["请输入平台基础要求", "Enter the platform foundation"], requiredSlot: ["请至少添加一个输出槽位", "Add at least one output slot"], invalidSlotKey: ["槽位键仅可使用小写字母、数字和下划线", "Use lowercase letters, numbers, and underscores"], slotKeyTooLong: ["槽位键最多 80 个字符", "Slot key must be 80 characters or fewer"], platformTooLong: ["目标平台最多 80 个字符", "Target platform must be 80 characters or fewer"], nameTooLong: ["名称最多 180 个字符", "Name must be 180 characters or fewer"], duplicateSlotKey: ["槽位键不可重复", "Slot keys must be unique"], requiredPromptFragment: ["请输入槽位提示要求", "Enter slot prompt requirements"], invalidLengthRange: ["最大长度不可小于最小长度", "Maximum length must not be below minimum"], requiredStyle: ["请至少启用一种图片风格", "Enable at least one image style"], tooManyStyles: ["图片风格最多 20 种", "Use at most 20 image styles"], duplicateStyle: ["图片风格不可重复", "Image styles must be unique"], defaultStyleNotAllowed: ["默认风格必须在允许列表中", "Default style must be allowed"] }; return map[code]?.[zh ? 0 : 1] ?? (zh ? "请检查此字段" : "Check this field"); }

function serverIssueText(code: string, fallback: string, zh: boolean) {
  const map: Record<string, [string, string]> = {
    name_zh_required: ["请输入模板中文名称。", "Chinese template name is required."],
    name_en_required: ["请输入模板英文名称。", "English template name is required."],
    target_platform_required: ["请输入目标平台。", "Target platform is required."],
    default_locale_required: ["请输入默认语言。", "Default locale is required."],
    prompt_compiler_version_required: ["请输入提示编译器版本。", "Prompt compiler version is required."],
    prompt_required: ["请输入提示片段。", "Prompt fragment is required."],
    prompt_secret_forbidden: ["提示内容疑似包含密钥，请移除后重试。", "Prompt content appears to contain a secret; remove it and retry."],
    template_variable_unknown: ["提示内容包含不支持的模板变量。", "Prompt content contains an unsupported template variable."],
    slot_key_required: ["请输入槽位键。", "Slot key is required."],
    slot_key_duplicate: ["槽位键在当前版本中必须唯一。", "Slot key must be unique within this version."],
    slot_kind_invalid: ["槽位类型不受支持。", "Slot kind is not supported."],
    slot_name_zh_required: ["请输入槽位中文名称。", "Chinese slot name is required."],
    slot_name_en_required: ["请输入槽位英文名称。", "English slot name is required."],
    slot_sequence_invalid: ["槽位顺序必须从 1 开始连续且不可重复。", "Slot sequence must be unique and contiguous from one."],
    constraints_object_required: ["槽位约束必须是 JSON 对象。", "Slot constraints must be a JSON object."],
    generation_config_object_required: ["生成配置必须是 JSON 对象。", "Generation configuration must be a JSON object."],
    layout_config_object_required: ["排版配置必须是 JSON 对象。", "Layout configuration must be a JSON object."],
    candidate_count_invalid: ["候选数量必须是 1 到 4 的整数。", "Candidate count must be an integer from 1 to 4."],
    allowed_candidate_count_invalid: ["允许的候选数量必须是 1 到 4 的不重复整数。", "Allowed candidate counts must be unique integers from 1 to 4."],
    allowed_sizes_invalid: ["允许的图片尺寸列表无效。", "Allowed image sizes are invalid."],
    allowed_qualities_invalid: ["允许的图片质量列表无效。", "Allowed image qualities are invalid."],
    allowed_styles_invalid: ["允许的图片风格列表无效。", "Allowed image styles are invalid."],
    allow_user_extra_prompt_invalid: ["用户附加提示开关必须是布尔值。", "User extra-prompt permission must be a boolean."],
    image_size_invalid: ["图片尺寸不受支持。", "Image size is not supported."],
    required_views_invalid: ["必需视角必须是不含空值的字符串数组。", "Required views must be an array of non-empty strings."],
    safe_area_invalid: ["文字安全区必须使用 0 到 1 之间的归一化坐标。", "Text safe area must use normalized coordinates between 0 and 1."],
  };
  return map[code]?.[zh ? 0 : 1] ?? (zh ? `校验未通过（${code}）` : fallback);
}

const zhText = { back: "返回模板列表", title: "新建 AI 内容模板", description: "建立稳定的平台层规则，并组合可独立选择的标题、搜索描述和图片槽位。", basics: "模板基础", nameZh: "中文名称", nameEn: "英文名称", platform: "目标平台", platformPrompt: "平台基础要求", platformPromptPlaceholder: "例如：Lazada 商品详情应移动端可读，不得虚构促销、评分或认证。", platformPromptRequired: "请输入平台基础要求", slots: "输出槽位", slotsHelp: "每个槽位会独立生成，用户创建任务时可自由选择。", addTitle: "添加标题", addSeo: "添加搜索描述", addImage: "添加图片", noSlots: "尚未添加槽位。", create: "创建草稿", created: "草稿已创建", createError: "创建失败，请检查内容后重试。", publication: "校验与发布", validate: "运行发布校验", validateFirst: "先运行服务端校验；只有完全通过后才可发布。", fixFormErrors: "保存并校验未执行：请先修正上方标出的必填项或格式问题。", valid: "校验通过，可以发布。", validationIssues: "发布前需要修正", publish: "发布版本", published: "已发布", actionError: "操作失败，请重试。", slot: "槽位", slotKey: "槽位键", slotNameZh: "槽位中文名", slotNameEn: "槽位英文名", prompt: "槽位提示要求", size: "图片尺寸", quality: "图片质量", candidates: "候选数量", minLength: "最小长度", maxLength: "最大长度", moveUp: "上移槽位", moveDown: "下移槽位", remove: "删除槽位", kinds: { image: "图片", title: "商品标题", seo_description: "搜索优化描述" } };
const enText: typeof zhText = { back: "Back to templates", title: "New AI content template", description: "Define stable platform direction and independently selectable title, search-description, and image slots.", basics: "Template basics", nameZh: "Chinese name", nameEn: "English name", platform: "Target platform", platformPrompt: "Platform foundation", platformPromptPlaceholder: "Example: Lazada product details must be mobile-readable and must not invent promotions, ratings, or certifications.", platformPromptRequired: "Enter the platform foundation", slots: "Output slots", slotsHelp: "Each slot is generated independently and can be selected when creating a job.", addTitle: "Add title", addSeo: "Add search description", addImage: "Add image", noSlots: "No slots added yet.", create: "Create draft", created: "Draft created", createError: "Creation failed. Check the content and try again.", publication: "Validate and publish", validate: "Run publication validation", validateFirst: "Run server validation first; publication is enabled only after a complete pass.", fixFormErrors: "Save and validation did not run: fix the required fields or format issues marked above.", valid: "Validation passed. This version can be published.", validationIssues: "Fix before publishing", publish: "Publish version", published: "Published", actionError: "The action failed. Try again.", slot: "Slot", slotKey: "Slot key", slotNameZh: "Chinese slot name", slotNameEn: "English slot name", prompt: "Slot prompt requirements", size: "Image size", quality: "Image quality", candidates: "Candidate count", minLength: "Minimum length", maxLength: "Maximum length", moveUp: "Move slot up", moveDown: "Move slot down", remove: "Remove slot", kinds: { image: "Image", title: "Product title", seo_description: "Search-optimized description" } };
