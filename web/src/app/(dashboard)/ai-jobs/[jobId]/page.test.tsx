import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
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

  it("edits, approves, previews, and explicitly applies a text candidate", async () => {
    const titleItem = { ...job.items[0], public_id: "item-title", slot_key: "title", kind: "title", slot_snapshot: { ...job.items[0].slot_snapshot, public_id: "slot-title", slot_key: "title", kind: "title", name: { zh: "商品标题", en: "Product title" } } };
    const textJob = { ...job, items: [titleItem] };
    const result = { public_id: "result-1", job_item_id: "item-title", candidate_index: 1, kind: "title", raw_structured: { title: "CargoFlow 透明手机壳", keywords: ["透明"], source_fields: ["product.brand"] }, validation: [], state: "candidate", edited_at: null, approved_at: null, rejected_at: null, applied_at: null, effective: false, created_at: job.created_at, updated_at: job.updated_at };
    const resultPath = "/api/proxy/ai-jobs/job-1/items/item-title/text-results/result-1";
    const editedStructured = { title: "CargoFlow 超薄透明手机壳", keywords: ["透明", "防摔"], source_fields: ["product.brand"] };
    const requests: Array<{ path: string; method: string; body?: unknown }> = [];
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
      const path = String(input);
      const method = init?.method ?? "GET";
      requests.push({ path, method, body: init?.body ? JSON.parse(String(init.body)) : undefined });
      if (path.endsWith("/ai-jobs/job-1")) return new Response(JSON.stringify(textJob), { status: 200 });
      if (path.endsWith("/ai-jobs/job-1/text-results")) return new Response(JSON.stringify({ data: [result] }), { status: 200 });
      if (path.endsWith(`${resultPath}/application-preview`)) return new Response(JSON.stringify({ before: {}, after: { title: "CargoFlow 超薄透明手机壳", revision: 1 } }), { status: 200 });
      if (path.endsWith(`${resultPath}/approve`)) return new Response(JSON.stringify({ ...result, edited_structured: editedStructured, state: "approved", effective: true }), { status: 200 });
      if (path.endsWith(`${resultPath}/apply`)) return new Response(JSON.stringify({ content: { public_id: "content-1", sku_id: 11, platform: "lazada", locale: "zh-CN", title: "CargoFlow 超薄透明手机壳", short_description: "", long_description: "", selling_points: [], search_keywords: [], revision: 1, updated_at: job.updated_at }, replayed: false }), { status: 200 });
      if (path.endsWith(resultPath) && method === "PATCH") return new Response(JSON.stringify({ ...result, edited_structured: (JSON.parse(String(init?.body)) as { structured: unknown }).structured }), { status: 200 });
      return new Response("not found", { status: 404 });
    });
    render(<AIJobDetailPage />, { wrapper: Providers });

    const title = await screen.findByDisplayValue("CargoFlow 透明手机壳");
    fireEvent.change(title, { target: { value: "CargoFlow 超薄透明手机壳" } });
    fireEvent.change(screen.getByLabelText("关键词（逗号分隔）"), { target: { value: "透明,防摔" } });
    fireEvent.click(screen.getByRole("button", { name: "保存编辑" }));
    await waitFor(() => expect(requests.some((request) => request.path.endsWith(resultPath) && request.method === "PATCH" && (request.body as { structured: { title: string } }).structured.title === "CargoFlow 超薄透明手机壳")).toBe(true));
    await waitFor(() => expect(screen.getByRole("button", { name: "批准候选" })).toBeEnabled());

    fireEvent.click(screen.getByRole("button", { name: "批准候选" }));
    const previewButton = await screen.findByRole("button", { name: "预览应用" });
    fireEvent.change(title, { target: { value: "尚未保存的新标题" } });
    expect(previewButton).toBeDisabled();
    fireEvent.change(title, { target: { value: "CargoFlow 超薄透明手机壳" } });
    expect(previewButton).toBeEnabled();
    fireEvent.click(previewButton);
    expect((await screen.findAllByText("应用后 · Revision 1")).length).toBeGreaterThan(0);
    fireEvent.click(screen.getByRole("button", { name: "应用到正式内容" }));
    await waitFor(() => expect(requests.some((request) => request.path.endsWith(`${resultPath}/approve`) && request.method === "POST")).toBe(true));
    expect(requests.some((request) => request.path.endsWith(`${resultPath}/application-preview`) && request.method === "GET")).toBe(true);
    await waitFor(() => expect(requests.some((request) => request.path.endsWith(`${resultPath}/apply`) && request.method === "POST")).toBe(true));
  });

  it("keeps commas inside SEO selling points and splits them only by line", async () => {
    const seoItem = { ...job.items[0], public_id: "item-seo", slot_key: "seo", kind: "seo_description", slot_snapshot: { ...job.items[0].slot_snapshot, public_id: "slot-seo", slot_key: "seo", kind: "seo_description", name: { zh: "搜索描述", en: "Search description" } } };
    const seoResult = { public_id: "result-seo", job_item_id: "item-seo", candidate_index: 1, kind: "seo_description", raw_structured: { short_description: "透明保护壳", selling_points: ["轻薄，易握"], long_description: "适合日常使用的透明保护壳。", search_keywords: ["透明壳"], source_fields: ["product.name"] }, validation: [], state: "candidate", edited_at: null, approved_at: null, rejected_at: null, applied_at: null, effective: false, created_at: job.created_at, updated_at: job.updated_at };
    const patchPath = "/api/proxy/ai-jobs/job-1/items/item-seo/text-results/result-seo";
    let patchBody: { structured?: { selling_points?: string[] } } | undefined;
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
      const path = String(input);
      if (path.endsWith("/ai-jobs/job-1")) return new Response(JSON.stringify({ ...job, items: [seoItem] }), { status: 200 });
      if (path.endsWith("/ai-jobs/job-1/text-results")) return new Response(JSON.stringify({ data: [seoResult] }), { status: 200 });
      if (path.endsWith(patchPath) && init?.method === "PATCH") {
        patchBody = JSON.parse(String(init.body)) as typeof patchBody;
        return new Response(JSON.stringify({ ...seoResult, edited_structured: patchBody?.structured }), { status: 200 });
      }
      return new Response("not found", { status: 404 });
    });
    render(<AIJobDetailPage />, { wrapper: Providers });

    const sellingPoints = await screen.findByLabelText("卖点（每行一条）");
    fireEvent.change(sellingPoints, { target: { value: "轻薄，易握\n防摔,耐用" } });
    fireEvent.click(screen.getByRole("button", { name: "保存编辑" }));
    await waitFor(() => expect(patchBody?.structured?.selling_points).toEqual(["轻薄，易握", "防摔,耐用"]));
  });
});
