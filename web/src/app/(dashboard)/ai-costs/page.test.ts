import { describe, expect, it } from "vitest";

import { ApiError } from "@/lib/api";

import { costErrorMessage, unpricedUsageDetail } from "./page";

describe("AI cost errors", () => {
  it("turns structured synchronization failures into actionable Chinese guidance", () => {
    expect(costErrorMessage(new ApiError("provider detail", 422, "openai_cost_permission_denied"))).toContain("组织成本读取权限");
    expect(costErrorMessage(new ApiError("provider detail", 503, "openai_cost_rate_limited"))).toContain("稍后重试");
    expect(costErrorMessage(new ApiError("provider detail", 409, "openai_cost_not_configured"))).toContain("OpenAI 设置");
  });

  it("preserves a safe fallback message for unrelated failures", () => {
    expect(costErrorMessage(new ApiError("safe message", 500, "other"))).toBe("safe message");
  });
});

describe("AI cost reconciliation copy", () => {
  it("documents that unpriced usage is excluded instead of blocking allocation", () => {
    expect(unpricedUsageDetail).toBe("仅统计，不参与任务级分摊");
  });
});
