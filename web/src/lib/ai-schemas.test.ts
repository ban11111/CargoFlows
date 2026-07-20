import { describe, expect, it } from "vitest";

import { aiTemplateDraftSchema, openAIKeySchema } from "./ai-schemas";

describe("openAIKeySchema", () => {
  it("trims a valid project key", () => {
    expect(openAIKeySchema.parse({ api_key: `  ${"x".repeat(20)}  ` })).toEqual({
      api_key: "x".repeat(20),
    });
  });

  it("rejects keys shorter than twenty characters", () => {
    const result = openAIKeySchema.safeParse({ api_key: "short" });
    expect(result.success).toBe(false);
    expect(result.error?.issues[0]?.message).toBe("openAIKeyTooShort");
  });

  it("rejects keys longer than the bounded in-memory input", () => {
    const result = openAIKeySchema.safeParse({ api_key: "x".repeat(513) });
    expect(result.success).toBe(false);
    expect(result.error?.issues[0]?.message).toBe("openAIKeyTooLong");
  });
});

describe("aiTemplateDraftSchema", () => {
  it("requires bilingual names and at least one valid discriminated slot", () => {
    const result = aiTemplateDraftSchema.safeParse({ name_zh: "Lazada 商品详情", name_en: "", target_platform: "lazada", slots: [] });
    expect(result.success).toBe(false);
  });

  it("accepts image, title, and SEO slot constraints", () => {
    const base = { name_zh: "中文", name_en: "English", target_platform: "lazada" };
    expect(aiTemplateDraftSchema.safeParse({ ...base, slots: [{ kind: "image", slot_key: "hero", name_zh: "主图", name_en: "Hero", prompt_fragment: "Create hero", size: "1024x1024", quality: "high", candidate_count: 2, style: "clean_white_background", allowed_styles: ["clean_white_background"] }] }).success).toBe(true);
    expect(aiTemplateDraftSchema.safeParse({ ...base, slots: [{ kind: "title", slot_key: "title", name_zh: "标题", name_en: "Title", prompt_fragment: "Write title", min_length: 10, max_length: 120 }] }).success).toBe(true);
    expect(aiTemplateDraftSchema.safeParse({ ...base, slots: [{ kind: "seo_description", slot_key: "seo", name_zh: "描述", name_en: "SEO", prompt_fragment: "Write SEO", max_length: 500 }] }).success).toBe(true);
  });

  it("enforces database-backed template and slot string limits", () => {
    const slot = { kind: "title", slot_key: `a${"b".repeat(80)}`, name_zh: "标题", name_en: "Title", prompt_fragment: "Write title", min_length: 10, max_length: 120 };
    expect(aiTemplateDraftSchema.safeParse({ name_zh: "中".repeat(181), name_en: "English", target_platform: "lazada", slots: [{ ...slot, slot_key: "title" }] }).success).toBe(false);
    expect(aiTemplateDraftSchema.safeParse({ name_zh: "中文", name_en: "English", target_platform: "p".repeat(81), slots: [{ ...slot, slot_key: "title" }] }).success).toBe(false);
    expect(aiTemplateDraftSchema.safeParse({ name_zh: "中文", name_en: "English", target_platform: "lazada", slots: [slot] }).success).toBe(false);
  });

  it("rejects duplicate slot keys", () => {
    const slot = { kind: "title" as const, slot_key: "title", name_zh: "标题", name_en: "Title", prompt_fragment: "Write title", min_length: 10, max_length: 120 };
    const result = aiTemplateDraftSchema.safeParse({ name_zh: "中文", name_en: "English", target_platform: "lazada", slots: [slot, { ...slot, name_en: "Second title" }] });
    expect(result.success).toBe(false);
    expect(result.error?.issues.some((issue) => issue.message === "duplicateSlotKey" && issue.path.join(".") === "slots.1.slot_key")).toBe(true);
  });
});
