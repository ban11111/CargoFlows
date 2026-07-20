"use client";

import { Check, LoaderCircle } from "lucide-react";
import { useState } from "react";
import type { FormEvent } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { dimensionLabel, modelFamilyDimensions, type ModelFamilyDimension, type ModelFamilyInput } from "@/lib/model-families";
import { useLanguage } from "@/lib/i18n";
import { cn } from "@/lib/utils";

export function ModelFamilyForm({ initial, busy, submitLabel, onCancel, onSubmit }: {
  initial: ModelFamilyInput;
  busy: boolean;
  submitLabel: string;
  onCancel: () => void;
  onSubmit: (value: ModelFamilyInput) => void;
}) {
  const { language } = useLanguage();
  const [value, setValue] = useState(initial);
  const [invariants, setInvariants] = useState(initial.common_structure.invariants.join("\n"));
  const text = language === "zh" ? {
    brand: "品牌", nameZh: "中文系列名称", nameEn: "英文系列名称", code: "系列代码", invariants: "共同结构（每行一项）",
    invariantHelp: "只写整个型号组都不改变的结构特征，例如“摄像头开孔位置一致”。", dimensions: "允许变化的维度", cancel: "取消",
  } : {
    brand: "Brand", nameZh: "Chinese family name", nameEn: "English family name", code: "Family code", invariants: "Shared structure (one per line)",
    invariantHelp: "List only structural facts that remain unchanged across the family.", dimensions: "Allowed variation dimensions", cancel: "Cancel",
  };
  const normalizedInvariants = invariants.split("\n").map((item) => item.trim()).filter(Boolean);
  const valid = value.brand.trim() && value.name_zh.trim() && value.name_en.trim() && value.model_code.trim() && normalizedInvariants.length > 0 && value.variation_dimensions.length > 0;

  function toggleDimension(dimension: ModelFamilyDimension) {
    setValue((current) => ({
      ...current,
      variation_dimensions: current.variation_dimensions.includes(dimension)
        ? current.variation_dimensions.filter((item) => item !== dimension)
        : [...current.variation_dimensions, dimension],
    }));
  }

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!valid) return;
    onSubmit({
      ...value,
      brand: value.brand.trim(), name_zh: value.name_zh.trim(), name_en: value.name_en.trim(), model_code: value.model_code.trim(),
      common_structure: { schema: "model_family_common_structure_v1", invariants: normalizedInvariants },
    });
  }

  return <form className="space-y-5" onSubmit={submit}>
    <div className="grid gap-4 sm:grid-cols-2">
      <Field id="family-brand" label={text.brand}><Input id="family-brand" maxLength={120} onChange={(event) => setValue({ ...value, brand: event.target.value })} required value={value.brand} /></Field>
      <Field id="family-code" label={text.code}><Input className="font-mono" id="family-code" maxLength={120} onChange={(event) => setValue({ ...value, model_code: event.target.value })} required value={value.model_code} /></Field>
      <Field id="family-name-zh" label={text.nameZh}><Input id="family-name-zh" maxLength={180} onChange={(event) => setValue({ ...value, name_zh: event.target.value })} required value={value.name_zh} /></Field>
      <Field id="family-name-en" label={text.nameEn}><Input id="family-name-en" maxLength={180} onChange={(event) => setValue({ ...value, name_en: event.target.value })} required value={value.name_en} /></Field>
    </div>
    <Field id="family-invariants" label={text.invariants} hint={text.invariantHelp}>
      <Textarea id="family-invariants" onChange={(event) => setInvariants(event.target.value)} required rows={4} value={invariants} />
    </Field>
    <fieldset>
      <legend className="text-sm font-semibold text-foreground">{text.dimensions}</legend>
      <div className="mt-2 flex flex-wrap gap-2">
        {modelFamilyDimensions.map((dimension) => {
          const selected = value.variation_dimensions.includes(dimension);
          return <button aria-pressed={selected} className={cn("flex min-h-11 items-center gap-2 rounded-lg border px-3 text-sm font-medium transition-colors", selected ? "border-primary/40 bg-primary/8 text-primary" : "border-border bg-card text-muted-foreground hover:border-primary/30 hover:text-foreground")} key={dimension} onClick={() => toggleDimension(dimension)} type="button">
            <span className={cn("grid h-5 w-5 place-items-center rounded border", selected ? "border-primary bg-primary text-white" : "border-border")} aria-hidden>{selected ? <Check className="h-3.5 w-3.5" /> : null}</span>
            {dimensionLabel(dimension, language)}
          </button>;
        })}
      </div>
    </fieldset>
    <div className="flex flex-wrap justify-end gap-2 border-t border-border pt-4">
      <Button disabled={busy} onClick={onCancel} type="button" variant="ghost">{text.cancel}</Button>
      <Button disabled={!valid || busy} type="submit">{busy ? <LoaderCircle className="h-4 w-4 animate-spin" /> : null}{submitLabel}</Button>
    </div>
  </form>;
}

function Field({ id, label, hint, children }: { id: string; label: string; hint?: string; children: React.ReactNode }) {
  return <div className="space-y-1.5"><Label htmlFor={id}>{label}</Label>{children}{hint ? <p className="text-xs leading-5 text-muted-foreground">{hint}</p> : null}</div>;
}
