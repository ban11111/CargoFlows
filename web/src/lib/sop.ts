import type { components } from "./openapi-types";

export type LocalizedText = components["schemas"]["LocalizedText"];
export type CaptureSOPSummary = components["schemas"]["CaptureSOPSummary"];
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

export function mergeSOPVersion(summary: CaptureSOPSummary, next: SOPVersion): CaptureSOPSummary {
  if (summary.public_id !== next.sop_public_id) return summary;
  let found = false;
  const versions = summary.versions.map((version) => {
    if (version.public_id === next.public_id) {
      found = true;
      return next;
    }
    return version;
  });
  if (!found) versions.push(next);
  return { ...summary, versions };
}
