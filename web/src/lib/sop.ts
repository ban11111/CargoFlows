import type { components } from "./openapi-types";

export type LocalizedText = components["schemas"]["LocalizedText"];
export type SOPVersion = components["schemas"]["SOPVersion"];
export type SOPView = components["schemas"]["SOPView"];
export type ValidationResponse = components["schemas"]["ValidationResponse"];

export type SOPPresetKey = NonNullable<components["schemas"]["AddViewRequest"]["preset_key"]>;
export type SOPViewPresetKey = "reference_front" | SOPPresetKey;

function defineExhaustivePresetKeys<const Keys extends readonly SOPPresetKey[]>(
  keys: Keys & (Exclude<SOPPresetKey, Keys[number]> extends never ? unknown : ["Missing generated SOP preset"]),
) {
  return keys;
}

export const addableSOPPresetKeys = defineExhaustivePresetKeys([
  "back",
  "left",
  "bottom",
  "right",
  "top",
  "detail_label",
  "packaging_front",
  "supplemental_info",
] as const);

export const sopPresetKeys = [
  "reference_front",
  ...addableSOPPresetKeys,
] as const satisfies readonly SOPViewPresetKey[];

export function localizedText(
  language: "zh" | "zh-CN" | "en",
  value: LocalizedText,
): string {
  const preferred = language === "en" ? value.en : value["zh-CN"];
  const fallback = language === "en" ? value["zh-CN"] : value.en;
  if (preferred.trim()) return preferred;
  if (fallback.trim()) return fallback;
  return "";
}
