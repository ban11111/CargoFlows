import { describe, expect, it } from "vitest";

import { openAIKeySchema } from "./ai-schemas";

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
