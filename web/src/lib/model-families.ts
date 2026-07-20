import type { components } from "@/lib/openapi-types";

export type ModelFamily = components["schemas"]["ModelFamily"];
export type ModelFamilyInput = components["schemas"]["CreateModelFamilyRequest"];
export type ModelFamilyDimension = ModelFamilyInput["variation_dimensions"][number];
export type SKU = components["schemas"]["SKU"];
export type VariantIdentity = components["schemas"]["VariantIdentityManifestVersion"];

export const modelFamilyDimensions: ModelFamilyDimension[] = [
  "color", "material", "finish", "texture", "trim", "ports", "controls", "labels", "accessories", "packaging", "other",
];

export const emptyModelFamilyInput: ModelFamilyInput = {
  brand: "",
  name_zh: "",
  name_en: "",
  model_code: "",
  common_structure: { schema: "model_family_common_structure_v1", invariants: [] },
  variation_dimensions: [],
};

export function familyName(family: Pick<ModelFamily, "name_zh" | "name_en">, language: "zh" | "en") {
  return language === "zh" ? family.name_zh : family.name_en;
}

export function dimensionLabel(value: ModelFamilyDimension, language: "zh" | "en") {
  const zh: Record<ModelFamilyDimension, string> = {
    color: "颜色", material: "材质", finish: "表面处理", texture: "纹理", trim: "饰边", ports: "接口", controls: "按键与控制", labels: "标签", accessories: "配件", packaging: "包装", other: "其他",
  };
  const en: Record<ModelFamilyDimension, string> = {
    color: "Color", material: "Material", finish: "Finish", texture: "Texture", trim: "Trim", ports: "Ports", controls: "Controls", labels: "Labels", accessories: "Accessories", packaging: "Packaging", other: "Other",
  };
  return language === "zh" ? zh[value] : en[value];
}
