import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { LanguageProvider } from "@/lib/i18n";
import { LanguageToggle } from "@/components/language-toggle";
import NewAIJobPage from "./page";

const push = vi.fn();
vi.mock("next/navigation", () => ({ useRouter: () => ({ push }) }));

function Providers({ children }: { children: ReactNode }) {
  return <LanguageProvider><LanguageToggle /><QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })}>{children}</QueryClientProvider></LanguageProvider>;
}

const template = {
  public_id: "template-1", name_zh: "Lazada 商品详情", name_en: "Lazada PDP", target_platform: "lazada", status: "active",
  created_at: "2026-07-17T00:00:00Z", updated_at: "2026-07-17T00:00:00Z",
  versions: [{ public_id: "version-1", version_number: 1, status: "published", default_locale: "zh-CN", prompt_compiler_version: "v1", platform_prompt: "platform", published_at: "2026-07-17T00:00:00Z", archived_at: null, created_at: "2026-07-17T00:00:00Z", updated_at: "2026-07-17T00:00:00Z", slots: [
    { public_id: "slot-hero", slot_key: "hero", kind: "image", name_zh: "白底主图", name_en: "White-background hero", description_zh: "商品主图", description_en: "Product hero", sequence: 1, optional: true, default_selected: false, prompt_fragment: "hero", constraints: { required_views: ["reference_front"] }, generation_config: { allowed_candidate_count: [1, 2], allowed_sizes: ["1024x1024"], allowed_qualities: ["high"], allowed_styles: ["简洁", "生活方式"], allow_user_extra_prompt: true }, layout_config: {} },
    { public_id: "slot-title", slot_key: "title", kind: "title", name_zh: "商品标题", name_en: "Product title", description_zh: "搜索标题", description_en: "Search title", sequence: 2, optional: true, default_selected: false, prompt_fragment: "title", constraints: {}, generation_config: { allowed_candidate_count: [1, 3] }, layout_config: {} },
  ] }],
};

function installWizardFetch(assetViewKey = "reference_front") {
  const requests: Array<{ path: string; init?: RequestInit; body?: unknown }> = [];
  const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
    const path = String(input);
    requests.push({ path, init, body: init?.body ? JSON.parse(String(init.body)) : undefined });
    if (path.endsWith("/skus")) return new Response(JSON.stringify({ data: [{ id: 11, code: "CF-CASE-CLR-IP17", status: "active", product: { name: "Clear Case" } }] }), { status: 200 });
    if (path.endsWith("/ai-content-templates")) return new Response(JSON.stringify({ data: [template] }), { status: 200 });
    if (path.includes("/assets/review/hierarchy")) return new Response(JSON.stringify({ data: [{ id: 1, name: "配件", name_en: "Accessories", is_system: false, skus: [{ id: 11, code: "CF-CASE-CLR-IP17", product_name: "Clear Case", tags: [], assets: [{ id: 31, original_url: "https://signed.invalid/private", thumbnail_url: "/thumb.jpg", review_status: "approved", captured_at: "2026-07-17T00:00:00Z", sop_view_key: assetViewKey, sop_view_name: { "zh-CN": "正面", en: "Front" }, photo_session_code: "PS-1" }] }] }] }), { status: 200 });
    if (path.endsWith("/ai-jobs") && init?.method === "POST") return new Response(JSON.stringify({ public_id: "job-created" }), { status: 201 });
    throw new Error(`Unexpected request: ${path}`);
  });
  return { fetchMock, requests };
}

describe("NewAIJobPage", () => {
  beforeEach(() => { push.mockReset(); window.localStorage.clear(); });

  it("submits only checked slots and shows the provider data disclosure", async () => {
    const { requests } = installWizardFetch();
    render(<NewAIJobPage />, { wrapper: Providers });

    fireEvent.change(await screen.findByLabelText("选择 SKU"), { target: { value: "11" } });
    fireEvent.change(screen.getByLabelText("选择模板版本"), { target: { value: "version-1" } });
    fireEvent.click(screen.getByRole("button", { name: "下一步" }));
    fireEvent.click(screen.getByRole("checkbox", { name: /白底主图/ }));
    fireEvent.click(screen.getByRole("button", { name: "下一步" }));
    expect(await screen.findByText("1 张已审核图片")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "下一步" }));
    fireEvent.change(screen.getByLabelText("风格"), { target: { value: "简洁" } });
    fireEvent.change(screen.getByLabelText("额外偏好（可选）"), { target: { value: "保留透明材质" } });
    fireEvent.click(screen.getByRole("button", { name: "下一步" }));

    expect(screen.getByText("商品数据和所选图片将发送给 OpenAI")).toBeInTheDocument();
    expect(screen.getByText("Dry-run：本阶段不会向 OpenAI 发送商品内容或图片")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "创建任务" }));

    await waitFor(() => expect(push).toHaveBeenCalledWith("/ai-jobs/job-created"));
    const post = requests.find((request) => request.path.endsWith("/ai-jobs") && request.init?.method === "POST");
    expect(post?.body).toMatchObject({ sku_id: 11, template_version_id: "version-1", selected_slot_keys: ["hero"], selected_asset_ids: [31], locale: "zh-CN", user_preference: "保留透明材质", generation_overrides: { hero: { candidate_count: 1, size: "1024x1024", quality: "high", style: "简洁" } } });
    expect(new Headers(post?.init?.headers).get("Idempotency-Key")).toMatch(/^ai-job-/);
  });

  it("blocks advancing until at least one optional slot is selected", async () => {
    installWizardFetch();
    render(<NewAIJobPage />, { wrapper: Providers });
    fireEvent.change(await screen.findByLabelText("选择 SKU"), { target: { value: "11" } });
    fireEvent.change(screen.getByLabelText("选择模板版本"), { target: { value: "version-1" } });
    fireEvent.click(screen.getByRole("button", { name: "下一步" }));
    fireEvent.click(screen.getByRole("button", { name: "下一步" }));
    expect(screen.getByRole("alert")).toHaveTextContent("请至少选择一个输出槽位");
    expect(screen.getByRole("heading", { name: "选择输出槽位" })).toBeInTheDocument();
  });

  it("blocks image slots until selected assets satisfy their required SOP views", async () => {
    installWizardFetch("back");
    render(<NewAIJobPage />, { wrapper: Providers });
    fireEvent.change(await screen.findByLabelText("选择 SKU"), { target: { value: "11" } });
    fireEvent.change(screen.getByLabelText("选择模板版本"), { target: { value: "version-1" } });
    fireEvent.click(screen.getByRole("button", { name: "下一步" }));
    fireEvent.click(screen.getByRole("checkbox", { name: /白底主图/ }));
    fireEvent.click(screen.getByRole("button", { name: "下一步" }));
    expect(await screen.findByText(/reference_front/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "下一步" }));
    expect(screen.getByRole("alert")).toHaveTextContent("必需视角");
    expect(screen.getByRole("heading", { name: "确认参考素材" })).toBeInTheDocument();
  });

  it("shows extra preference only when every selected slot permits it", async () => {
    installWizardFetch();
    render(<NewAIJobPage />, { wrapper: Providers });
    fireEvent.change(await screen.findByLabelText("选择 SKU"), { target: { value: "11" } });
    fireEvent.change(screen.getByLabelText("选择模板版本"), { target: { value: "version-1" } });
    fireEvent.click(screen.getByRole("button", { name: "下一步" }));
    fireEvent.click(screen.getByRole("checkbox", { name: /白底主图/ }));
    fireEvent.click(screen.getByRole("checkbox", { name: /商品标题/ }));
    fireEvent.click(screen.getByRole("button", { name: "下一步" }));
    fireEvent.click(screen.getByRole("button", { name: "下一步" }));
    expect(screen.queryByLabelText("额外偏好（可选）")).not.toBeInTheDocument();
  });

  it("retranslates an active wizard error immediately", async () => {
    installWizardFetch();
    render(<NewAIJobPage />, { wrapper: Providers });
    await screen.findByLabelText("选择 SKU");
    fireEvent.click(screen.getByRole("button", { name: "下一步" }));
    expect(screen.getByRole("alert")).toHaveTextContent("请选择 SKU");
    fireEvent.click(screen.getByRole("button", { name: "语言" }));
    expect(screen.getByRole("alert")).toHaveTextContent("Choose a SKU");
  });
});
