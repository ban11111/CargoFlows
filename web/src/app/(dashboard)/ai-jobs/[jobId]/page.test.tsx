import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";

import { LanguageProvider } from "@/lib/i18n";
import { jobPollingInterval } from "@/lib/ai-job-polling";
import AIJobDetailPage from "./page";

vi.mock("next/navigation", () => ({ useParams: () => ({ jobId: "job-1" }) }));

function Providers({ children }: { children: ReactNode }) {
  return <LanguageProvider><QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>{children}</QueryClientProvider></LanguageProvider>;
}

const job = {
  public_id: "job-1", sku_id: 11, template_version_id: "version-1", target_platform: "lazada", locale: "zh-CN", status: "completed", snapshot_schema: "cargoflow_product_generation_v1", started_at: "2026-07-17T00:00:01Z", completed_at: "2026-07-17T00:00:02Z", cancelled_at: null, created_at: "2026-07-17T00:00:00Z", updated_at: "2026-07-17T00:00:02Z",
  input_snapshot: { schema: "cargoflow_product_generation_v1", locale: "zh-CN", target_platform: "lazada", product: { name: "Clear Case", brand: "CargoFlow", description: "Private long description", category: { name_zh: "配件", name_en: "Accessories" } }, sku: { code: "CF-CASE-CLR-IP17", color: "透明", size: "iPhone 17", platform_title: "Private title", selling_points: "Private claims", tags: ["clear"] }, sop: { public_id: "sop-1", version_public_id: "sop-v1", version_number: 1, schema_version: "v1", name: { zh: "手机壳拍摄", en: "Case capture" }, description: { zh: "private", en: "private" }, coordinate_system: "pcs_object_v1", views: [] }, template: { template_public_id: "template-1", version_public_id: "version-1", version_number: 1, prompt_compiler_version: "v1", platform_prompt: "SECRET PLATFORM PROMPT", selected_slots: [] }, selected_assets: [{ id: 31, object_key: "private/key.jpg", original_url: "https://signed.invalid/private", thumbnail_url: "/thumb.jpg", captured_at: "2026-07-17T00:00:00Z", view: {} }], user_preference: "PRIVATE USER PREFERENCE", generation_overrides: {} },
  items: [{ public_id: "item-1", slot_key: "hero", kind: "image", status: "completed", slot_snapshot: { public_id: "slot-1", slot_key: "hero", kind: "image", name: { zh: "白底主图", en: "White-background hero" }, description: { zh: "商品主图", en: "Product hero" }, sequence: 1, optional: true, default_selected: false, prompt_fragment: "PRIVATE PROMPT", constraints: {}, generation_config: {}, layout_config: {} }, selected_input_asset_ids: [31], attempt_count: 1, safe_error: "", started_at: "2026-07-17T00:00:01Z", completed_at: "2026-07-17T00:00:02Z", created_at: "2026-07-17T00:00:00Z", updated_at: "2026-07-17T00:00:02Z" }],
};

describe("AIJobDetailPage", () => {
  it("polls active items every two seconds and stops for terminal items", () => {
    expect(jobPollingInterval({ ...job, status: "running", items: [{ ...job.items[0], status: "running" }] } as never)).toBe(2000);
    expect(jobPollingInterval(job as never)).toBe(false);
  });

  it("shows item status and a redacted snapshot summary", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(JSON.stringify(job), { status: 200 }));
    render(<AIJobDetailPage />, { wrapper: Providers });
    expect(await screen.findByRole("heading", { name: /CF-CASE-CLR-IP17/ })).toBeInTheDocument();
    expect(screen.getByText("白底主图")).toBeInTheDocument();
    expect(screen.getByText("1 张已审核图片")).toBeInTheDocument();
    expect(screen.getByText("手机壳拍摄 · V1")).toBeInTheDocument();
    expect(screen.queryByText(/SECRET PLATFORM PROMPT/)).not.toBeInTheDocument();
    expect(screen.queryByText(/PRIVATE USER PREFERENCE/)).not.toBeInTheDocument();
    expect(screen.queryByText(/signed\.invalid/)).not.toBeInTheDocument();
    expect(screen.queryByText(/private\/key/)).not.toBeInTheDocument();
  });
});
