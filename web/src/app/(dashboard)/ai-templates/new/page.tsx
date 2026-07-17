"use client";

import { useMutation } from "@tanstack/react-query";
import { ArrowDown, ArrowLeft, ArrowUp, CheckCircle2, ImageIcon, LoaderCircle, Plus, Search, Send, Trash2, Type } from "lucide-react";
import Link from "next/link";
import { FormEvent, useState } from "react";

import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { aiTemplateDraftSchema, type AITemplateSlotInput } from "@/lib/ai-schemas";
import { ApiError, apiRequest } from "@/lib/api";
import { useLanguage } from "@/lib/i18n";
import type { components } from "@/lib/openapi-types";

type Template = components["schemas"]["AIContentTemplate"];
type Version = components["schemas"]["AIContentTemplateVersion"];
type Validation = components["schemas"]["AITemplateValidationResponse"];
type SlotKind = AITemplateSlotInput["kind"];
type EditableSlot = AITemplateSlotInput & { client_id: string };

export default function NewAITemplatePage() {
  const { language } = useLanguage();
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
  const errors = Object.fromEntries(Object.entries(errorCodes).map(([path, code]) => [path, localError(code, zh)]));
  const fingerprint = JSON.stringify(mutationPayload(nameZh, nameEn, platform, platformPrompt, slots));
  const currentValidation = validatedFingerprint === fingerprint ? validation : null;

  const create = useMutation({
    mutationFn: (payload: components["schemas"]["AIContentTemplateMutationRequest"]) => apiRequest<Template>("/ai-content-templates", { method: "POST", body: JSON.stringify(payload) }),
    onSuccess(template) { setVersion(template.versions[0] ?? null); setValidation(null); },
  });
  const validate = useMutation({
    mutationFn: async () => {
      const updated = await apiRequest<Version>(`/ai-content-template-versions/${version?.public_id}`, { method: "PATCH", body: JSON.stringify(mutationPayload(nameZh, nameEn, platform, platformPrompt, slots)) });
      setVersion(updated);
      return apiRequest<Validation>(`/ai-content-template-versions/${version?.public_id}/validate`, { method: "POST" });
    },
    onSuccess(response) { setValidation(response); setValidatedFingerprint(fingerprint); },
  });
  const publish = useMutation({
    mutationFn: () => apiRequest<Version>(`/ai-content-template-versions/${version?.public_id}/publish`, { method: "POST" }),
    onSuccess(next) { setVersion(next); },
    onError(error) {
      const response = parseValidationError(error);
      if (response) { setValidation(response); setValidatedFingerprint(fingerprint); }
    },
  });

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
    if (Object.keys(nextErrors).length || !parsed.success) return;
    validate.mutate();
  }

  function addSlot(kind: SlotKind) { setSlots((current) => [...current, defaultSlot(kind, nextSlotNumber(kind, current))]); }
  function updateSlot(index: number, patch: Partial<AITemplateSlotInput>) { setSlots((current) => current.map((slot, slotIndex) => slotIndex === index ? ({ ...slot, ...patch } as EditableSlot) : slot)); }
  function move(index: number, direction: -1 | 1) { const target = index + direction; if (target < 0 || target >= slots.length) return; setSlots((current) => { const next = [...current]; [next[index], next[target]] = [next[target], next[index]]; return next; }); setValidation(null); }

  return <div className="mx-auto max-w-5xl space-y-6"><header><Button asChild className="mb-3 min-h-11" variant="ghost"><Link href="/ai-templates"><ArrowLeft className="h-4 w-4" />{text.back}</Link></Button><p className="text-xs font-semibold uppercase tracking-[0.14em] text-primary">CargoFlow · AI</p><h1 className="mt-2 text-2xl font-semibold tracking-tight">{text.title}</h1><p className="mt-1 max-w-2xl text-sm leading-6 text-muted-foreground">{text.description}</p></header><form className="space-y-6" onSubmit={submit}><Card><CardHeader><CardTitle>{text.basics}</CardTitle></CardHeader><CardContent className="grid gap-4 md:grid-cols-2"><Field error={errors.name_zh} id="template-name-zh" label={text.nameZh}><Input className="h-11" id="template-name-zh" maxLength={180} onChange={(event) => setNameZh(event.target.value)} value={nameZh} /></Field><Field error={errors.name_en} id="template-name-en" label={text.nameEn}><Input className="h-11" id="template-name-en" maxLength={180} onChange={(event) => setNameEn(event.target.value)} value={nameEn} /></Field><Field error={errors.target_platform} id="template-platform" label={text.platform}><Input className="h-11" id="template-platform" maxLength={80} onChange={(event) => setPlatform(event.target.value)} value={platform} /></Field><div className="space-y-1.5 md:col-span-2"><Label htmlFor="platform-prompt">{text.platformPrompt}</Label><Textarea id="platform-prompt" onChange={(event) => setPlatformPrompt(event.target.value)} placeholder={text.platformPromptPlaceholder} value={platformPrompt} />{errors.platform_prompt ? <p className="text-sm text-danger" role="alert">{errors.platform_prompt}</p> : null}</div></CardContent></Card><Card><CardHeader><div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between"><div><CardTitle>{text.slots}</CardTitle><p className="mt-1 text-sm text-muted-foreground">{text.slotsHelp}</p></div><div className="flex flex-wrap gap-2"><AddButton icon={Type} label={text.addTitle} onClick={() => addSlot("title")} /><AddButton icon={Search} label={text.addSeo} onClick={() => addSlot("seo_description")} /><AddButton icon={ImageIcon} label={text.addImage} onClick={() => addSlot("image")} /></div></div></CardHeader><CardContent className="space-y-4">{slots.length === 0 ? <div className="rounded-lg border border-dashed border-border p-8 text-center text-sm text-muted-foreground">{text.noSlots}</div> : null}{slots.map((slot, index) => <SlotEditor errors={errors} index={index} key={slot.client_id} onMove={move} onRemove={() => setSlots((current) => current.filter((_, itemIndex) => itemIndex !== index))} onUpdate={updateSlot} slot={slot} text={text} />)}{errors.slots ? <p className="text-sm text-danger" role="alert">{errors.slots}</p> : null}</CardContent></Card>{create.isError ? <p className="rounded-md border border-danger/30 bg-danger/5 p-3 text-sm text-danger" role="alert">{text.createError}</p> : null}<div className="flex justify-end"><Button className="min-h-11" disabled={create.isPending || Boolean(version)} type="submit">{create.isPending ? <LoaderCircle className="h-4 w-4 animate-spin motion-reduce:animate-none" /> : <Plus className="h-4 w-4" />}{version ? text.created : text.create}</Button></div></form>{version ? <Card><CardHeader><CardTitle>{text.publication}</CardTitle></CardHeader><CardContent className="space-y-4"><div className="flex flex-wrap items-center gap-3"><span className="rounded-full border border-warning/30 bg-warning/5 px-3 py-1 text-xs font-semibold text-warning">V{version.version_number} · {version.status}</span><Button className="min-h-11" disabled={validate.isPending || version.status !== "draft"} onClick={validateDraft} variant="secondary"><CheckCircle2 className="h-4 w-4" />{text.validate}</Button><Button className="min-h-11" disabled={publish.isPending || currentValidation?.code !== "template_valid" || version.status !== "draft"} onClick={() => publish.mutate()}><Send className="h-4 w-4" />{version.status === "published" ? text.published : text.publish}</Button></div>{currentValidation ? currentValidation.code === "template_valid" ? <p className="text-sm text-success" role="status">{text.valid}</p> : <IssueList issues={currentValidation.issues} title={text.validationIssues} /> : <p className="text-sm text-muted-foreground">{text.validateFirst}</p>}{validate.isError || publish.isError ? <p className="text-sm text-danger" role="alert">{text.actionError}</p> : null}</CardContent></Card> : null}</div>;
}

function SlotEditor({ slot, index, onUpdate, onMove, onRemove, errors, text }: { slot: AITemplateSlotInput; index: number; onUpdate: (index: number, patch: Partial<AITemplateSlotInput>) => void; onMove: (index: number, direction: -1 | 1) => void; onRemove: () => void; errors: Record<string, string>; text: typeof zhText }) {
  const prefix = `slots.${index}`;
  return <section className="rounded-lg border border-border bg-muted/20 p-4" aria-label={`${text.slot} ${index + 1}`}><div className="mb-4 flex items-center justify-between gap-3"><div><strong>{index + 1}. {text.kinds[slot.kind]}</strong><p className="text-xs text-muted-foreground">{slot.kind}</p></div><div className="flex gap-1"><IconButton label={text.moveUp} onClick={() => onMove(index, -1)}><ArrowUp className="h-4 w-4" /></IconButton><IconButton label={text.moveDown} onClick={() => onMove(index, 1)}><ArrowDown className="h-4 w-4" /></IconButton><IconButton label={text.remove} onClick={onRemove}><Trash2 className="h-4 w-4" /></IconButton></div></div><div className="grid gap-4 md:grid-cols-2"><Field error={errors[`${prefix}.slot_key`]} id={`${prefix}-key`} label={text.slotKey}><Input id={`${prefix}-key`} maxLength={80} onChange={(event) => onUpdate(index, { slot_key: event.target.value })} value={slot.slot_key} /></Field><Field error={errors[`${prefix}.name_zh`]} id={`${prefix}-zh`} label={text.slotNameZh}><Input id={`${prefix}-zh`} maxLength={180} onChange={(event) => onUpdate(index, { name_zh: event.target.value })} value={slot.name_zh} /></Field><Field error={errors[`${prefix}.name_en`]} id={`${prefix}-en`} label={text.slotNameEn}><Input id={`${prefix}-en`} maxLength={180} onChange={(event) => onUpdate(index, { name_en: event.target.value })} value={slot.name_en} /></Field><div className="space-y-1.5 md:col-span-2"><Label htmlFor={`${prefix}-prompt`}>{text.prompt}</Label><Textarea id={`${prefix}-prompt`} onChange={(event) => onUpdate(index, { prompt_fragment: event.target.value })} value={slot.prompt_fragment} />{errors[`${prefix}.prompt_fragment`] ? <p className="text-sm text-danger">{errors[`${prefix}.prompt_fragment`]}</p> : null}</div>{slot.kind === "image" ? <><Field id={`${prefix}-size`} label={text.size}><select className="h-11 w-full rounded-md border border-border bg-card px-3" id={`${prefix}-size`} onChange={(event) => onUpdate(index, { size: event.target.value as "1024x1024" })} value={slot.size}><option>1024x1024</option><option>1536x1024</option><option>1024x1536</option></select></Field><Field id={`${prefix}-quality`} label={text.quality}><select className="h-11 w-full rounded-md border border-border bg-card px-3" id={`${prefix}-quality`} onChange={(event) => onUpdate(index, { quality: event.target.value as "high" })} value={slot.quality}><option value="low">low</option><option value="medium">medium</option><option value="high">high</option></select></Field><Field id={`${prefix}-count`} label={text.candidates}><Input id={`${prefix}-count`} max={4} min={1} onChange={(event) => onUpdate(index, { candidate_count: Number(event.target.value) })} inputMode="numeric" type="number" value={slot.candidate_count} /></Field></> : null}{slot.kind === "title" ? <><Field id={`${prefix}-min`} label={text.minLength}><Input id={`${prefix}-min`} min={1} onChange={(event) => onUpdate(index, { min_length: Number(event.target.value) })} inputMode="numeric" type="number" value={slot.min_length} /></Field><Field id={`${prefix}-max`} label={text.maxLength}><Input id={`${prefix}-max`} max={500} min={1} onChange={(event) => onUpdate(index, { max_length: Number(event.target.value) })} inputMode="numeric" type="number" value={slot.max_length} /></Field></> : null}{slot.kind === "seo_description" ? <Field id={`${prefix}-seo-max`} label={text.maxLength}><Input id={`${prefix}-seo-max`} max={10000} min={1} onChange={(event) => onUpdate(index, { max_length: Number(event.target.value) })} inputMode="numeric" type="number" value={slot.max_length} /></Field> : null}</div></section>;
}

function Field({ id, label, error, children }: { id: string; label: string; error?: string; children: React.ReactNode }) { return <div className="space-y-1.5"><Label htmlFor={id}>{label}</Label>{children}{error ? <p className="text-sm text-danger" role="alert">{error}</p> : null}</div>; }
function AddButton({ icon: Icon, label, onClick }: { icon: typeof Plus; label: string; onClick: () => void }) { return <Button className="min-h-11" onClick={onClick} type="button" variant="secondary"><Icon className="h-4 w-4" />{label}</Button>; }
function IconButton({ label, onClick, children }: { label: string; onClick: () => void; children: React.ReactNode }) { return <Button aria-label={label} className="min-h-11 min-w-11" onClick={onClick} size="icon" type="button" variant="ghost">{children}</Button>; }
function IssueList({ issues, title }: { issues: Validation["issues"]; title: string }) { const { language } = useLanguage(); return <div className="rounded-lg border border-danger/30 bg-danger/5 p-4" role="alert"><p className="font-medium text-danger">{title}</p><ul className="mt-2 space-y-1 text-sm text-danger">{issues.map((issue, index) => <li key={`${issue.path}-${index}`}><code>{issue.path}</code>: {serverIssueText(issue.code, issue.message, language === "zh")}</li>)}</ul></div>; }

function defaultSlot(kind: SlotKind, number: number): EditableSlot {
  const base = { client_id: crypto.randomUUID(), slot_key: `${kind === "seo_description" ? "seo" : kind}_${number}`, name_zh: "", name_en: "", prompt_fragment: "" };
  if (kind === "image") return { ...base, kind, size: "1024x1024", quality: "high", candidate_count: 1 };
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
  const base = { slot_key: slot.slot_key, kind: slot.kind, name_zh: slot.name_zh, name_en: slot.name_en, description_zh: "", description_en: "", sequence: sequence + 1, optional: true, default_selected: false, prompt_fragment: slot.prompt_fragment, layout_config: {} };
  if (slot.kind === "image") return { ...base, constraints: {}, generation_config: { size: slot.size, quality: slot.quality, candidate_count: slot.candidate_count } };
  if (slot.kind === "title") return { ...base, constraints: { min_length: slot.min_length, max_length: slot.max_length }, generation_config: {} };
  return { ...base, constraints: { max_length: slot.max_length }, generation_config: {} };
}

function localError(code: string, zh: boolean) { const map: Record<string, [string, string]> = { requiredNameZh: ["请输入中文名称", "Enter a Chinese name"], requiredNameEn: ["请输入英文名称", "Enter an English name"], requiredPlatform: ["请输入目标平台", "Enter a target platform"], requiredPlatformPrompt: ["请输入平台基础要求", "Enter the platform foundation"], requiredSlot: ["请至少添加一个输出槽位", "Add at least one output slot"], invalidSlotKey: ["槽位键仅可使用小写字母、数字和下划线", "Use lowercase letters, numbers, and underscores"], slotKeyTooLong: ["槽位键最多 80 个字符", "Slot key must be 80 characters or fewer"], platformTooLong: ["目标平台最多 80 个字符", "Target platform must be 80 characters or fewer"], nameTooLong: ["名称最多 180 个字符", "Name must be 180 characters or fewer"], duplicateSlotKey: ["槽位键不可重复", "Slot keys must be unique"], requiredPromptFragment: ["请输入槽位提示要求", "Enter slot prompt requirements"], invalidLengthRange: ["最大长度不可小于最小长度", "Maximum length must not be below minimum"] }; return map[code]?.[zh ? 0 : 1] ?? (zh ? "请检查此字段" : "Check this field"); }

function serverIssueText(code: string, fallback: string, zh: boolean) { const map: Record<string, [string, string]> = { slot_key_duplicate: ["槽位键在当前版本中必须唯一。", "Slot key must be unique within this version."], slot_sequence_invalid: ["槽位顺序必须从 1 开始连续且不可重复。", "Slot sequence must be unique and contiguous from one."], template_secret_detected: ["提示内容疑似包含密钥，请移除后重试。", "Prompt content appears to contain a secret; remove it and retry."], prompt_variable_unsupported: ["提示内容包含不支持的变量。", "Prompt content contains an unsupported variable."], image_size_invalid: ["图片尺寸不受支持。", "Image size is not supported."] }; return map[code]?.[zh ? 0 : 1] ?? (zh ? `校验未通过（${code}）` : fallback); }

const zhText = { back: "返回模板列表", title: "新建 AI 内容模板", description: "建立稳定的平台层规则，并组合可独立选择的标题、搜索描述和图片槽位。", basics: "模板基础", nameZh: "中文名称", nameEn: "英文名称", platform: "目标平台", platformPrompt: "平台基础要求", platformPromptPlaceholder: "例如：Lazada 商品详情应移动端可读，不得虚构促销、评分或认证。", platformPromptRequired: "请输入平台基础要求", slots: "输出槽位", slotsHelp: "每个槽位会独立生成，用户创建任务时可自由选择。", addTitle: "添加标题", addSeo: "添加搜索描述", addImage: "添加图片", noSlots: "尚未添加槽位。", create: "创建草稿", created: "草稿已创建", createError: "创建失败，请检查内容后重试。", publication: "校验与发布", validate: "运行发布校验", validateFirst: "先运行服务端校验；只有完全通过后才可发布。", valid: "校验通过，可以发布。", validationIssues: "发布前需要修正", publish: "发布版本", published: "已发布", actionError: "操作失败，请重试。", slot: "槽位", slotKey: "槽位键", slotNameZh: "槽位中文名", slotNameEn: "槽位英文名", prompt: "槽位提示要求", size: "图片尺寸", quality: "图片质量", candidates: "候选数量", minLength: "最小长度", maxLength: "最大长度", moveUp: "上移槽位", moveDown: "下移槽位", remove: "删除槽位", kinds: { image: "图片", title: "商品标题", seo_description: "搜索优化描述" } };
const enText: typeof zhText = { back: "Back to templates", title: "New AI content template", description: "Define stable platform direction and independently selectable title, search-description, and image slots.", basics: "Template basics", nameZh: "Chinese name", nameEn: "English name", platform: "Target platform", platformPrompt: "Platform foundation", platformPromptPlaceholder: "Example: Lazada product details must be mobile-readable and must not invent promotions, ratings, or certifications.", platformPromptRequired: "Enter the platform foundation", slots: "Output slots", slotsHelp: "Each slot is generated independently and can be selected when creating a job.", addTitle: "Add title", addSeo: "Add search description", addImage: "Add image", noSlots: "No slots added yet.", create: "Create draft", created: "Draft created", createError: "Creation failed. Check the content and try again.", publication: "Validate and publish", validate: "Run publication validation", validateFirst: "Run server validation first; publication is enabled only after a complete pass.", valid: "Validation passed. This version can be published.", validationIssues: "Fix before publishing", publish: "Publish version", published: "Published", actionError: "The action failed. Try again.", slot: "Slot", slotKey: "Slot key", slotNameZh: "Chinese slot name", slotNameEn: "English slot name", prompt: "Slot prompt requirements", size: "Image size", quality: "Image quality", candidates: "Candidate count", minLength: "Minimum length", maxLength: "Maximum length", moveUp: "Move slot up", moveDown: "Move slot down", remove: "Remove slot", kinds: { image: "Image", title: "Product title", seo_description: "Search-optimized description" } };
