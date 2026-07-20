import { describe, expect, it } from "vitest";

import { imageStyleKeys, imageStyleLabel, imageStylePresets } from "./image-styles";

describe("image style catalog", () => {
  it("exposes the stable twenty-key bilingual catalog", () => {
    expect(imageStyleKeys).toHaveLength(20);
    expect(new Set(imageStyleKeys).size).toBe(20);
    expect(imageStyleKeys[0]).toBe("clean_white_background");
    expect(imageStyleLabel("premium_dark", "zh")).toBe("高级暗调");
    expect(imageStyleLabel("premium_dark", "en")).toBe("Premium dark");
    expect(imageStylePresets.every((preset) => preset.description.zh && preset.description.en)).toBe(true);
  });

  it("keeps legacy custom values readable", () => {
    expect(imageStyleLabel("legacy-custom-style", "zh")).toBe("legacy-custom-style");
  });
});
