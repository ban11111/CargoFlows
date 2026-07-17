import { z } from "zod";

export const openAIKeySchema = z.object({
  api_key: z
    .string()
    .trim()
    .min(20, "openAIKeyTooShort")
    .max(512, "openAIKeyTooLong"),
});

export type OpenAIKeyInput = z.infer<typeof openAIKeySchema>;
