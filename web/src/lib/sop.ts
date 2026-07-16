import type { components } from "./openapi-types";

export type LocalizedText = components["schemas"]["LocalizedText"];
export type SOPVersion = components["schemas"]["SOPVersion"];
export type SOPView = components["schemas"]["SOPView"];
export type ValidationResponse = components["schemas"]["ValidationResponse"];

export type SOPPresetKey =
  | "reference_front"
  | "back"
  | "left"
  | "bottom"
  | "right"
  | "top"
  | "detail_label"
  | "packaging_front";

export const sopPresetKeys = [
  "reference_front",
  "back",
  "left",
  "bottom",
  "right",
  "top",
  "detail_label",
  "packaging_front",
] as const satisfies readonly SOPPresetKey[];

export function localizedText(
  language: "zh" | "zh-CN" | "en",
  value: LocalizedText,
): string {
  return language === "en" ? value.en : value["zh-CN"];
}
