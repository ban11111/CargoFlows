import { z } from "zod";

export const openAIKeySchema = z.object({
  api_key: z
    .string()
    .trim()
    .min(20, "openAIKeyTooShort")
    .max(512, "openAIKeyTooLong"),
});

export type OpenAIKeyInput = z.infer<typeof openAIKeySchema>;

const baseSlot = z.object({
  slot_key: z.string().trim().regex(/^[a-z][a-z0-9_]*$/, "invalidSlotKey"),
  name_zh: z.string().trim().min(1, "requiredNameZh"),
  name_en: z.string().trim().min(1, "requiredNameEn"),
  prompt_fragment: z.string().trim().min(1, "requiredPromptFragment"),
});

const imageSlot = baseSlot.extend({
  kind: z.literal("image"),
  size: z.enum(["1024x1024", "1536x1024", "1024x1536"]),
  quality: z.enum(["low", "medium", "high"]),
  candidate_count: z.coerce.number().int().min(1).max(4),
});

const titleSlot = baseSlot.extend({
  kind: z.literal("title"),
  min_length: z.coerce.number().int().min(1),
  max_length: z.coerce.number().int().max(500),
}).refine((slot) => slot.max_length >= slot.min_length, { message: "invalidLengthRange", path: ["max_length"] });

const seoSlot = baseSlot.extend({
  kind: z.literal("seo_description"),
  max_length: z.coerce.number().int().min(1).max(10000),
});

export const aiTemplateSlotSchema = z.union([imageSlot, titleSlot, seoSlot]);

export const aiTemplateDraftSchema = z.object({
  name_zh: z.string().trim().min(1, "requiredNameZh"),
  name_en: z.string().trim().min(1, "requiredNameEn"),
  target_platform: z.string().trim().min(1, "requiredPlatform"),
  slots: z.array(aiTemplateSlotSchema).min(1, "requiredSlot"),
}).superRefine((draft, context) => {
  const seen = new Set<string>();
  draft.slots.forEach((slot, index) => {
    if (seen.has(slot.slot_key)) context.addIssue({ code: "custom", message: "duplicateSlotKey", path: ["slots", index, "slot_key"] });
    seen.add(slot.slot_key);
  });
});

export type AITemplateDraftInput = z.infer<typeof aiTemplateDraftSchema>;
export type AITemplateSlotInput = z.infer<typeof aiTemplateSlotSchema>;
