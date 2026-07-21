import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { LanguageProvider } from "@/lib/i18n";
import { LanguageToggle } from "@/components/language-toggle";
import NewAIJobPage from "./page";

const push = vi.fn();
vi.mock("next/navigation", () => ({ useRouter: () => ({ push }) }));

const skuPublicID = "11111111-1111-4111-8111-111111111111";
const assetPublicID = "22222222-2222-4222-8222-222222222222";
const brandPublicID = "33333333-3333-4333-8333-333333333333";
const brandIconPublicID = "44444444-4444-4444-8444-444444444444";

function Providers({ children }: { children: ReactNode }) {
  return <LanguageProvider><LanguageToggle /><QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })}>{children}</QueryClientProvider></LanguageProvider>;
}

const template = {
  public_id: "template-1", name_zh: "Lazada 商品详情", name_en: "Lazada PDP", target_platform: "lazada", status: "active",
  created_at: "2026-07-17T00:00:00Z", updated_at: "2026-07-17T00:00:00Z",
  versions: [{ public_id: "version-1", version_number: 1, status: "published", default_locale: "zh-CN", prompt_compiler_version: "v1", platform_prompt: "platform", published_at: "2026-07-17T00:00:00Z", archived_at: null, created_at: "2026-07-17T00:00:00Z", updated_at: "2026-07-17T00:00:00Z", slots: [
    { public_id: "slot-hero", slot_key: "hero", kind: "image", name_zh: "白底主图", name_en: "White-background hero", description_zh: "商品主图", description_en: "Product hero", sequence: 1, optional: true, default_selected: false, prompt_fragment: "hero", constraints: { required_views: ["reference_front"] }, generation_config: { allowed_candidate_count: [1, 2], allowed_sizes: ["1024x1024"], allowed_qualities: ["high"], allowed_styles: ["简洁", "生活方式"], allow_user_extra_prompt: true }, layout_config: {} },
    { public_id: "slot-title", slot_key: "title", kind: "title", name_zh: "商品标题", name_en: "Product title", description_zh: "搜索标题", description_en: "Search title", sequence: 2, optional: true, default_selected: false, prompt_fragment: "title", constraints: {}, generation_config: { allowed_candidate_count: [1, 3] }, layout_config: {} },
    { public_id: "slot-detail", slot_key: "detail", kind: "image", name_zh: "细节卖点", name_en: "Detail benefits", description_zh: "展示边缘和按键", description_en: "Show edges and buttons", sequence: 3, optional: true, default_selected: false, prompt_fragment: "detail", constraints: { required_views: ["reference_front"] }, generation_config: { allowed_candidate_count: [1, 2], allowed_sizes: ["1024x1024"], allowed_qualities: ["high"], allowed_styles: ["简洁", "生活方式"], allow_user_extra_prompt: true }, layout_config: {} },
    { public_id: "slot-lifestyle", slot_key: "lifestyle", kind: "image", name_zh: "生活场景", name_en: "Lifestyle", description_zh: "展示使用场景", description_en: "Show product in use", sequence: 4, optional: true, default_selected: false, prompt_fragment: "lifestyle", constraints: { required_views: ["reference_front"] }, generation_config: { allowed_candidate_count: [1, 2], allowed_sizes: ["1024x1024"], allowed_qualities: ["high"], allowed_styles: ["简洁", "生活方式"], allow_user_extra_prompt: true }, layout_config: {} },
    { public_id: "slot-installed", slot_key: "installed", kind: "image", name_zh: "真实手机装壳棚拍图", name_en: "Installed device studio", description_zh: "展示真实装机效果", description_en: "Show the installed device", sequence: 5, optional: true, default_selected: false, prompt_fragment: "installed", constraints: { required_views: ["reference_front"], requires_compatible_device_model: true }, generation_config: { allowed_candidate_count: [1], allowed_sizes: ["1024x1024"], allowed_qualities: ["high"], allowed_styles: ["简洁"], allow_user_extra_prompt: true }, layout_config: {} },
  ] }],
};

function installWizardFetch(assetViewKey = "reference_front", compatibleDeviceModel = "iPhone 17 Pro") {
  const requests: Array<{ path: string; init?: RequestInit; body?: unknown }> = [];
  const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
    const path = String(input);
    requests.push({ path, init, body: init?.body ? JSON.parse(String(init.body)) : undefined });
    if (path.endsWith("/skus")) return new Response(JSON.stringify({ data: [{ public_id: skuPublicID, code: "CF-CASE-CLR-IP17", status: "active", compatible_device_model: compatibleDeviceModel, product: { name: "Clear Case", brand: "CargoFlows", brand_id: brandPublicID } }] }), { status: 200 });
	if (path.includes(`/brands/${brandPublicID}/icons`)) return new Response(JSON.stringify({ data: [{ public_id: brandIconPublicID, name: "Primary", notes: "", media_url: `/api/v1/brand-icons/${brandIconPublicID}/media`, status: "active" }] }), { status: 200 });
    if (path.endsWith("/ai-content-templates")) return new Response(JSON.stringify({ data: [template] }), { status: 200 });
    if (path.includes("/assets/review/hierarchy")) return new Response(JSON.stringify({ data: [{ id: 1, name: "配件", name_en: "Accessories", is_system: false, skus: [{ public_id: skuPublicID, code: "CF-CASE-CLR-IP17", product_name: "Clear Case", tags: [], assets: [{ public_id: assetPublicID, media_url: `/api/v1/assets/${assetPublicID}/media`, review_status: "approved", captured_at: "2026-07-17T00:00:00Z", sop_view_key: assetViewKey, sop_view_name: { "zh-CN": "正面", en: "Front" }, photo_session_code: "PS-1" }] }] }] }), { status: 200 });
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

    fireEvent.change(await screen.findByLabelText("选择 SKU"), { target: { value: skuPublicID } });
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
    expect(screen.getByText("真实任务：标题和搜索描述会发送给 OpenAI")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "创建任务" }));

    await waitFor(() => expect(push).toHaveBeenCalledWith("/ai-jobs/job-created"));
    const post = requests.find((request) => request.path.endsWith("/ai-jobs") && request.init?.method === "POST");
    expect(post?.body).toMatchObject({ sku_id: skuPublicID, template_version_id: "version-1", selected_slot_keys: ["hero"], selected_asset_ids: [assetPublicID], selected_brand_icon_ids: [brandIconPublicID], output_locales: ["en", "zh-CN"], user_preference: "保留透明材质", generation_overrides: { hero: { candidate_count: 1, size: "1024x1024", quality: "high", style: "简洁" } } });
    expect(post?.body).not.toHaveProperty("locale");
    expect(new Headers(post?.init?.headers).get("Idempotency-Key")).toMatch(/^ai-job-/);
  });

  it.each([
    ["English", ["en"]],
    ["简体中文", ["zh-CN"]],
  ])("submits the %s output language strategy", async (option, outputLocales) => {
    const { requests } = installWizardFetch();
    render(<NewAIJobPage />, { wrapper: Providers });

    fireEvent.change(await screen.findByLabelText("选择 SKU"), { target: { value: skuPublicID } });
    fireEvent.change(screen.getByLabelText("选择模板版本"), { target: { value: "version-1" } });
    expect(screen.getByLabelText("输出语言策略")).toHaveValue("bilingual");
    fireEvent.change(screen.getByLabelText("输出语言策略"), { target: { value: outputLocales[0] } });
    fireEvent.click(screen.getByRole("button", { name: "下一步" }));
    fireEvent.click(screen.getByRole("checkbox", { name: /商品标题/ }));
    fireEvent.click(screen.getByRole("button", { name: "下一步" }));
    fireEvent.click(screen.getByRole("button", { name: "下一步" }));
    fireEvent.click(screen.getByRole("button", { name: "下一步" }));
    fireEvent.click(screen.getByRole("button", { name: "创建任务" }));

    await waitFor(() => expect(push).toHaveBeenCalledWith("/ai-jobs/job-created"));
    const post = requests.find((request) => request.path.endsWith("/ai-jobs") && request.init?.method === "POST");
    expect(post?.body).toMatchObject({ output_locales: outputLocales });
  });

  it("blocks advancing until at least one optional slot is selected", async () => {
    installWizardFetch();
    render(<NewAIJobPage />, { wrapper: Providers });
    fireEvent.change(await screen.findByLabelText("选择 SKU"), { target: { value: skuPublicID } });
    fireEvent.change(screen.getByLabelText("选择模板版本"), { target: { value: "version-1" } });
    fireEvent.click(screen.getByRole("button", { name: "下一步" }));
    fireEvent.click(screen.getByRole("button", { name: "下一步" }));
    expect(screen.getByRole("alert")).toHaveTextContent("请至少选择一个输出槽位");
    expect(screen.getByRole("heading", { name: "选择输出槽位" })).toBeInTheDocument();
  });

  it("disables device-dependent slots when the SKU has no compatible model", async () => {
    installWizardFetch("reference_front", "   ");
    render(<NewAIJobPage />, { wrapper: Providers });
    fireEvent.change(await screen.findByLabelText("选择 SKU"), { target: { value: skuPublicID } });
    fireEvent.change(screen.getByLabelText("选择模板版本"), { target: { value: "version-1" } });
    fireEvent.click(screen.getByRole("button", { name: "下一步" }));
    expect(screen.getByRole("checkbox", { name: /真实手机装壳棚拍图/ })).toBeDisabled();
    expect(screen.getByText("请先编辑 SKU 并填写兼容设备型号。")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "编辑 SKU" })).toHaveAttribute("href", `/skus/${skuPublicID}`);
  });

  it("builds multiple canvases and allows a project to be reused", async () => {
    const { requests } = installWizardFetch();
    render(<NewAIJobPage />, { wrapper: Providers });
    fireEvent.change(await screen.findByLabelText("选择 SKU"), { target: { value: skuPublicID } });
    fireEvent.change(screen.getByLabelText("选择模板版本"), { target: { value: "version-1" } });
    fireEvent.click(screen.getByRole("button", { name: "下一步" }));
    fireEvent.click(screen.getByRole("checkbox", { name: /白底主图/ }));
    fireEvent.click(screen.getByRole("checkbox", { name: /细节卖点/ }));
    fireEvent.click(screen.getByRole("checkbox", { name: /生活场景/ }));
    fireEvent.click(screen.getByRole("radio", { name: /自由编排多张画布/ }));
    fireEvent.click(screen.getByRole("button", { name: "添加画布" }));
    const detailCheckboxes = screen.getAllByRole("checkbox", { name: /细节卖点/ });
    fireEvent.click(detailCheckboxes[1]);
    const lifestyleCheckboxes = screen.getAllByRole("checkbox", { name: /生活场景/ });
    fireEvent.click(lifestyleCheckboxes[lifestyleCheckboxes.length - 1]);

    fireEvent.click(screen.getByRole("button", { name: "下一步" }));
    fireEvent.click(screen.getByRole("button", { name: "下一步" }));
    expect(screen.getByRole("heading", { name: "画布 1 设置" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "画布 2 设置" })).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "下一步" }));
    expect(screen.getByText(/2 张自定义画布，将生成 2 张图片/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "创建任务" }));

    await waitFor(() => expect(push).toHaveBeenCalledWith("/ai-jobs/job-created"));
    const post = requests.find((request) => request.path.endsWith("/ai-jobs") && request.init?.method === "POST");
    expect(post?.body).toMatchObject({
      selected_slot_keys: ["hero", "detail", "lifestyle"],
      image_canvases: [
        { canvas_key: expect.any(String), slot_keys: ["hero", "detail"], generation_override: expect.any(Object) },
        { canvas_key: expect.any(String), slot_keys: ["detail", "lifestyle"], generation_override: expect.any(Object) },
      ],
    });
    expect(post?.body).not.toHaveProperty("image_composition");
  });

  it("blocks image slots until selected assets satisfy their required SOP views", async () => {
    installWizardFetch("back");
    render(<NewAIJobPage />, { wrapper: Providers });
    fireEvent.change(await screen.findByLabelText("选择 SKU"), { target: { value: skuPublicID } });
    fireEvent.change(screen.getByLabelText("选择模板版本"), { target: { value: "version-1" } });
    fireEvent.click(screen.getByRole("button", { name: "下一步" }));
    fireEvent.click(screen.getByRole("checkbox", { name: /白底主图/ }));
    fireEvent.click(screen.getByRole("button", { name: "下一步" }));
    expect(await screen.findByText(/reference_front/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "下一步" }));
    expect(screen.getByRole("alert")).toHaveTextContent("必需视角");
    expect(screen.getByRole("heading", { name: "确认参考素材" })).toBeInTheDocument();
  });

  it("groups supplemental assets as facts and does not count them as required product views", async () => {
    installWizardFetch("supplemental_info");
    render(<NewAIJobPage />, { wrapper: Providers });
    fireEvent.change(await screen.findByLabelText("选择 SKU"), { target: { value: skuPublicID } });
    fireEvent.change(screen.getByLabelText("选择模板版本"), { target: { value: "version-1" } });
    fireEvent.click(screen.getByRole("button", { name: "下一步" }));
    fireEvent.click(screen.getByRole("checkbox", { name: /白底主图/ }));
    fireEvent.click(screen.getByRole("button", { name: "下一步" }));

    expect(await screen.findByText("补充资料")).toBeInTheDocument();
    expect(screen.getByText(/仅作为卖点、规格和说明书中的可见事实来源/)).toBeInTheDocument();
    expect(screen.getByText(/reference_front/)).toBeInTheDocument();
  });

  it("shows extra preference only when every selected slot permits it", async () => {
    installWizardFetch();
    render(<NewAIJobPage />, { wrapper: Providers });
    fireEvent.change(await screen.findByLabelText("选择 SKU"), { target: { value: skuPublicID } });
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
