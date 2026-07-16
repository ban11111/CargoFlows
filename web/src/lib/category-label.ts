import type { Language } from "@/lib/i18n";

export interface LocalizedCategory {
  name: string;
  name_en?: string;
}

export function categoryLabel(category: LocalizedCategory, language: Language) {
  return language === "en" && category.name_en?.trim() ? category.name_en : category.name;
}
