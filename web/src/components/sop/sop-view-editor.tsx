"use client";

import { ArrowDown, ArrowUp, ImagePlus, LockKeyhole, Save, Trash2 } from "lucide-react";
import Image from "next/image";
import type { ChangeEvent } from "react";
import { useState } from "react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { authenticatedMediaURL } from "@/lib/api";
import type { Language } from "@/lib/i18n";
import { localizedText, type LocalizedText, type SOPView } from "@/lib/sop";

type VectorKey = "camera_position_direction" | "image_up_direction" | "target";

interface SOPViewEditorProps {
  view: SOPView;
  language: Language;
  locked: boolean;
  immutable?: boolean;
  busy?: boolean;
  aggregateLocked?: boolean;
  moveUpDisabled?: boolean;
  moveDownDisabled?: boolean;
  saveDisabled?: boolean;
  errorPaths?: ReadonlySet<string>;
  onChange: (view: SOPView) => void;
  onDelete: () => void;
  onSave?: () => void;
  onMove?: (direction: -1 | 1) => void;
  onReferenceUpload?: (file: File, caption: LocalizedText) => void;
  onReferenceDelete?: (imageID: string) => void;
  onReferenceMove?: (imageID: string, direction: -1 | 1) => void;
}

const copy = {
  zh: {
    required: "必拍", locked: "顺序、类型、必拍状态和方向已锁定", standard: "标准视图", detail: "细节视图",
    chineseName: "中文名称", englishName: "English name", chineseInstruction: "中文拍摄说明", englishInstruction: "English instruction",
    camera: "相机位置方向", up: "画面向上方向", target: "目标点", occupancy: "画面占比", ratio: "宽高比",
    rotate: "允许旋转校正", multiple: "允许同一视图上传多张", delete: "删除", save: "保存", moveUp: "上移", moveDown: "下移",
    references: "参考图", addReference: "添加参考图", removeReference: "删除参考图", referenceUp: "参考图上移", referenceDown: "参考图下移",
  },
  en: {
    required: "Required", locked: "Order, kind, required state, and pose are locked", standard: "Standard view", detail: "Detail view",
    chineseName: "Chinese name", englishName: "English name", chineseInstruction: "Chinese instruction", englishInstruction: "English instruction",
    camera: "Camera position direction", up: "Image-up direction", target: "Target", occupancy: "Frame occupancy", ratio: "Aspect ratio",
    rotate: "Allow rotation correction", multiple: "Allow multiple captures for this view", delete: "Delete", save: "Save", moveUp: "Move up", moveDown: "Move down",
    references: "Reference images", addReference: "Add reference image", removeReference: "Delete reference image", referenceUp: "Move reference image up", referenceDown: "Move reference image down",
  },
} as const;

export function SOPViewEditor({
  view, language, locked, immutable = false, busy = false, aggregateLocked = false, moveUpDisabled = false, moveDownDisabled = false, saveDisabled = false, errorPaths = new Set(), onChange, onDelete, onSave,
  onMove, onReferenceUpload, onReferenceDelete, onReferenceMove,
}: SOPViewEditorProps) {
  const c = copy[language];
  const [referenceCaption, setReferenceCaption] = useState<LocalizedText>({ "zh-CN": "", en: "" });
  const displayName = localizedText(language, view.name) || `${language === "zh" ? "视图" : "View"} ${view.sequence}`;
  const controlName = view.preset_key === "packaging_front" ? view.name.en : displayName;
  const allDisabled = immutable || busy;
  const poseLocked = locked || allDisabled;
  const pathHasError = (suffix: string) => [...errorPaths].some((path) => path.endsWith(suffix));

  function updateVector(key: VectorKey, index: number, raw: string) {
    const vector = [...view.pose[key]] as [number, number, number];
    vector[index] = raw === "" ? Number.NaN : Number(raw);
    onChange({ ...view, pose: { ...view.pose, [key]: vector } });
  }

  function vectorFields(key: VectorKey, label: string) {
    return (
      <fieldset className="min-w-0">
        <legend className="mb-2 text-xs font-semibold text-muted-foreground">{label}</legend>
        <div className="grid grid-cols-3 gap-2 font-mono tabular-nums">
          {(["X", "Y", "Z"] as const).map((axis, index) => {
            const inputLabel = `${controlName} ${label} ${axis}`;
            return (
              <div className="min-w-0" key={axis}>
                <Label className="sr-only" htmlFor={`${view.public_id}-${key}-${axis}`}>{inputLabel}</Label>
                <Input
                  aria-invalid={pathHasError(`pose.${key}`) || undefined}
                  aria-label={inputLabel}
                  className="h-11 min-w-0 px-2 font-mono tabular-nums"
                  disabled={poseLocked || (key === "target" && view.view_kind === "standard")}
                  id={`${view.public_id}-${key}-${axis}`}
                  inputMode="decimal"
                  onChange={(event) => updateVector(key, index, event.target.value)}
                  step="any"
                  type="number"
                  value={Number.isNaN(view.pose[key][index]) ? "" : view.pose[key][index]}
                />
              </div>
            );
          })}
        </div>
      </fieldset>
    );
  }

  return (
    <Card className="scroll-mt-24" data-testid={`sop-view-${view.public_id}`} id={`view-${view.public_id}`}>
      <CardHeader>
        <div className="flex flex-wrap items-center justify-between gap-2">
          <div className="flex min-w-0 items-center gap-2">
            <span className="grid h-8 w-8 shrink-0 place-items-center rounded-md bg-foreground font-mono text-sm font-semibold tabular-nums text-white" aria-label={`${language === "zh" ? "顺序" : "Sequence"} ${view.sequence}`}>
              {String(view.sequence).padStart(2, "0")}
            </span>
            <CardTitle className="truncate">{displayName}</CardTitle>
            <Badge variant={view.required ? "success" : "neutral"}>{view.required ? c.required : language === "zh" ? "选拍" : "Optional"}</Badge>
            {locked ? <Badge variant="neutral"><LockKeyhole className="h-3 w-3" />{language === "zh" ? "锁定基准" : "Locked reference"}</Badge> : null}
          </div>
          <div className="flex gap-2">
            <Button aria-label={`${controlName}${c.moveUp}`} className="min-h-11 min-w-11" disabled={allDisabled || aggregateLocked || moveUpDisabled} onClick={() => onMove?.(-1)} size="icon" type="button" variant="ghost"><ArrowUp className="h-4 w-4" /></Button>
            <Button aria-label={`${controlName}${c.moveDown}`} className="min-h-11 min-w-11" disabled={allDisabled || aggregateLocked || moveDownDisabled} onClick={() => onMove?.(1)} size="icon" type="button" variant="ghost"><ArrowDown className="h-4 w-4" /></Button>
            <Button aria-label={`${c.delete}${displayName}`} className="min-h-11 min-w-11" disabled={allDisabled || aggregateLocked || locked} onClick={onDelete} size="icon" type="button" variant="ghost"><Trash2 className="h-4 w-4" /></Button>
          </div>
        </div>
        {locked ? <p className="mt-2 flex items-center gap-2 text-xs text-muted-foreground"><LockKeyhole className="h-3.5 w-3.5" />{c.locked}</p> : null}
      </CardHeader>
      <CardContent className="space-y-5">
        <div className="grid gap-4 md:grid-cols-2">
          <Field label={c.chineseName} id={`${view.public_id}-name-zh`}>
            <Input aria-invalid={pathHasError("name.zh-CN") || undefined} className="h-11" disabled={busy} id={`${view.public_id}-name-zh`} onChange={(e) => onChange({ ...view, name: { ...view.name, "zh-CN": e.target.value } })} readOnly={immutable} value={view.name["zh-CN"]} />
          </Field>
          <Field label={c.englishName} id={`${view.public_id}-name-en`}>
            <Input aria-invalid={pathHasError("name.en") || undefined} className="h-11" disabled={busy} id={`${view.public_id}-name-en`} onChange={(e) => onChange({ ...view, name: { ...view.name, en: e.target.value } })} readOnly={immutable} value={view.name.en} />
          </Field>
          <Field label={c.chineseInstruction} id={`${view.public_id}-instruction-zh`}>
            <Textarea disabled={busy} id={`${view.public_id}-instruction-zh`} onChange={(e) => onChange({ ...view, instruction: { ...view.instruction, "zh-CN": e.target.value } })} readOnly={immutable} value={view.instruction["zh-CN"]} />
          </Field>
          <Field label={c.englishInstruction} id={`${view.public_id}-instruction-en`}>
            <Textarea disabled={busy} id={`${view.public_id}-instruction-en`} onChange={(e) => onChange({ ...view, instruction: { ...view.instruction, en: e.target.value } })} readOnly={immutable} value={view.instruction.en} />
          </Field>
        </div>

        <div className="grid gap-3 sm:grid-cols-3">
          <Field label={language === "zh" ? "视图类型" : "View kind"} id={`${view.public_id}-kind`}>
            <select className="h-11 w-full rounded-md border border-border bg-card px-3 text-sm outline-none focus:border-primary focus:ring-2 focus:ring-primary/20" disabled={poseLocked} id={`${view.public_id}-kind`} onChange={(e) => onChange({ ...view, view_kind: e.target.value as SOPView["view_kind"], pose: { ...view.pose, target: [0, 0, 0] } })} value={view.view_kind}>
              <option value="standard">{c.standard}</option><option value="detail">{c.detail}</option>
            </select>
          </Field>
          <label className="flex min-h-11 items-center gap-2 self-end rounded-md border border-border px-3 text-sm">
            <input aria-label={`${controlName} ${c.required}`} checked={view.required} disabled={poseLocked} onChange={(e) => onChange({ ...view, required: e.target.checked })} type="checkbox" />
            {c.required}
          </label>
          <Field label={c.ratio} id={`${view.public_id}-ratio`}>
            <select className="h-11 w-full rounded-md border border-border bg-card px-3 text-sm outline-none focus:border-primary focus:ring-2 focus:ring-primary/20" disabled={allDisabled} id={`${view.public_id}-ratio`} onChange={(e) => onChange({ ...view, composition: { ...view.composition, aspect_ratio: e.target.value } })} value={view.composition.aspect_ratio}>
              {["1:1", "4:5", "3:4", "16:9"].map((ratio) => <option key={ratio}>{ratio}</option>)}
            </select>
          </Field>
        </div>

        <div className="grid gap-4 lg:grid-cols-3">
          {vectorFields("camera_position_direction", c.camera)}
          {vectorFields("image_up_direction", c.up)}
          {vectorFields("target", c.target)}
        </div>

        <div className="grid gap-3 sm:grid-cols-2">
          <Field label={c.occupancy} id={`${view.public_id}-occupancy`}>
            <Input className="h-11" disabled={allDisabled} id={`${view.public_id}-occupancy`} max="1" min="0.01" onChange={(e) => onChange({ ...view, composition: { ...view.composition, frame_occupancy: Number(e.target.value) } })} step="0.01" type="number" value={view.composition.frame_occupancy} />
          </Field>
          <label className="flex min-h-11 items-center gap-2 self-end rounded-md border border-border px-3 text-sm"><input checked={view.composition.allow_rotation_correction} disabled={allDisabled} onChange={(e) => onChange({ ...view, composition: { ...view.composition, allow_rotation_correction: e.target.checked } })} type="checkbox" />{c.rotate}</label>
          <label className="flex min-h-11 items-center gap-2 rounded-md border border-border px-3 text-sm"><input checked={view.allow_multiple} disabled={poseLocked} onChange={(e) => onChange({ ...view, allow_multiple: e.target.checked })} type="checkbox" />{c.multiple}</label>
        </div>

        <section aria-labelledby={`${view.public_id}-references`} className="rounded-md bg-muted p-3">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <h3 className="text-xs font-semibold" id={`${view.public_id}-references`}>{c.references}</h3>
            <label className={`inline-flex min-h-11 items-center gap-2 rounded-md border border-border bg-card px-3 text-sm font-medium ${allDisabled || aggregateLocked ? "cursor-not-allowed opacity-50" : "cursor-pointer"}`}>
              <ImagePlus className="h-4 w-4" />{c.addReference}
              <input accept="image/*" aria-label={`${language === "zh" ? "为" : "Add reference image for "}${displayName}${language === "zh" ? "添加参考图" : ""}`} className="sr-only" disabled={allDisabled || aggregateLocked} onChange={(event: ChangeEvent<HTMLInputElement>) => { const file = event.target.files?.[0]; if (file) onReferenceUpload?.(file, referenceCaption); event.target.value = ""; }} type="file" />
            </label>
          </div>
          <div className="mt-3 grid gap-2 sm:grid-cols-2">
            <Field id={`${view.public_id}-reference-caption-zh`} label={`${displayName}参考图中文说明`}>
              <Input className="h-11" disabled={allDisabled} id={`${view.public_id}-reference-caption-zh`} onChange={(event) => setReferenceCaption({ ...referenceCaption, "zh-CN": event.target.value })} value={referenceCaption["zh-CN"]} />
            </Field>
            <Field id={`${view.public_id}-reference-caption-en`} label={`${view.name.en} reference caption`}>
              <Input className="h-11" disabled={allDisabled} id={`${view.public_id}-reference-caption-en`} onChange={(event) => setReferenceCaption({ ...referenceCaption, en: event.target.value })} value={referenceCaption.en} />
            </Field>
          </div>
          {view.reference_images.length ? (
            <ul className="mt-3 grid gap-3 sm:grid-cols-2">
              {view.reference_images.map((image, index) => <li className="flex gap-3 rounded-md border border-border bg-card p-2" key={image.public_id}>
                <Image alt={localizedText(language, image.caption) || `${displayName} ${c.references} ${index + 1}`} className="h-20 w-20 rounded object-cover" height={80} src={authenticatedMediaURL(image.thumbnail_url)} unoptimized width={80} />
                <div className="min-w-0 flex-1"><p className="truncate text-xs">{localizedText(language, image.caption) || image.public_id}</p><div className="mt-2 flex gap-1">
                  <Button aria-label={`${c.referenceUp} ${index + 1}`} className="min-h-11 min-w-11" disabled={allDisabled || aggregateLocked || index === 0} onClick={() => onReferenceMove?.(image.public_id, -1)} size="icon" type="button" variant="ghost"><ArrowUp className="h-4 w-4" /></Button>
                  <Button aria-label={`${c.referenceDown} ${index + 1}`} className="min-h-11 min-w-11" disabled={allDisabled || aggregateLocked || index === view.reference_images.length - 1} onClick={() => onReferenceMove?.(image.public_id, 1)} size="icon" type="button" variant="ghost"><ArrowDown className="h-4 w-4" /></Button>
                  <Button aria-label={`${c.removeReference} ${index + 1}`} className="min-h-11 min-w-11" disabled={allDisabled || aggregateLocked} onClick={() => onReferenceDelete?.(image.public_id)} size="icon" type="button" variant="ghost"><Trash2 className="h-4 w-4" /></Button>
                </div></div>
              </li>)}
            </ul>
          ) : <p className="mt-3 text-xs text-muted-foreground">{language === "zh" ? "尚未添加参考图" : "No reference images yet"}</p>}
        </section>

        <div className="flex justify-end"><Button aria-label={`${c.save}${displayName}`} className="min-h-11" disabled={allDisabled || saveDisabled} onClick={onSave} type="button"><Save className="h-4 w-4" />{c.save}{displayName}</Button></div>
      </CardContent>
    </Card>
  );
}

function Field({ label, id, children }: { label: string; id: string; children: React.ReactNode }) {
  return <div className="space-y-1.5"><Label htmlFor={id}>{label}</Label>{children}</div>;
}
