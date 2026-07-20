import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { LanguageProvider } from "@/lib/i18n";
import CapturePage from "./page";

vi.mock("@/lib/capture-image", async () => {
  const actual = await vi.importActual<typeof import("@/lib/capture-image")>("@/lib/capture-image");
  return {
    ...actual,
    normalizeCaptureImage: vi.fn(async (file: File) => ({
      file: new File(["optimized"], file.name.replace(/\.[^.]+$/, ".jpg"), { type: "image/jpeg" }),
      width: 1_000,
      height: 1_000,
    })),
  };
});

const skuID = "11111111-1111-4111-8111-111111111111";
const versionID = "22222222-2222-4222-8222-222222222222";
const olderVersionID = "33333333-3333-4333-8333-333333333333";
const frontID = "44444444-4444-4444-8444-444444444444";
const detailID = "55555555-5555-4555-8555-555555555555";
const sessionID = "66666666-6666-4666-8666-666666666666";

const pose = {
  space: "object",
  camera_position_direction: { x: 0, y: 0, z: 1 },
  image_up_direction: { x: 1, y: 0, z: 0 },
  target: { x: 0, y: 0, z: 0 },
};
const composition = { frame_occupancy: 0.8, aspect_ratio: "1:1", allow_rotation_correction: true, allow_mirror: false };
const views = [
  { public_id: frontID, sequence: 1, role: "reference_front", view_kind: "standard", name: { "zh-CN": "基准正面", en: "Reference front" }, instruction: { "zh-CN": "保持商品垂直", en: "Keep the product upright" }, required: true, allow_multiple: false, pose, composition, reference_images: [] },
  { public_id: detailID, sequence: 2, role: "capture", view_kind: "detail", name: { "zh-CN": "标签细节", en: "Label detail" }, instruction: { "zh-CN": "拍清标签", en: "Keep the label sharp" }, required: false, allow_multiple: true, pose, composition, reference_images: [] },
];

function Providers({ children }: { children: ReactNode }) {
  return <LanguageProvider><QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>{children}</QueryClientProvider></LanguageProvider>;
}

function response(value: unknown, status = 200) {
  return new Response(value === undefined ? undefined : JSON.stringify(value), { status, headers: { "content-type": "application/json" } });
}

function choose(select: HTMLElement, value: string) {
  const element = select as HTMLSelectElement;
  for (const option of element.options) option.selected = option.value === value;
  fireEvent.change(element);
}

async function chooseSKU() {
  await screen.findByRole("option", { name: /CF-CASE/ });
  const select = screen.getByLabelText("选择 SKU");
  choose(select, skuID);
  return select;
}

function installFetch({ failFirstPut = false } = {}) {
  const requests: Array<{ url: string; method: string; body?: unknown }> = [];
  let putCount = 0;
  vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
    const url = String(input);
    const method = init?.method ?? "GET";
    requests.push({ url, method, body: typeof init?.body === "string" ? JSON.parse(init.body) : undefined });
    if (url.endsWith("/api/proxy/skus")) return response({ data: [
      { public_id: skuID, code: "CF-CASE", status: "active", product: { name: "Clear Case", category_id: 7 }, tags: [] },
      { public_id: "draft", code: "DRAFT", status: "draft", product: { name: "Draft", category_id: 7 }, tags: [] },
    ] });
    if (url.includes("/api/proxy/capture-sops?category_id=7")) return response({ data: [{ public_id: "sop", category_id: 7, versions: [
      { public_id: olderVersionID, sop_public_id: "sop", schema_version: "1.0", version_number: 1, status: "published", name: { "zh-CN": "旧版", en: "Older" }, description: { "zh-CN": "", en: "" }, coordinate_system: {}, updated_at: "2026-01-01T00:00:00Z", published_at: "2026-01-01T00:00:00Z", views },
      { public_id: versionID, sop_public_id: "sop", schema_version: "1.0", version_number: 2, status: "published", name: { "zh-CN": "手机壳拍摄", en: "Case capture" }, description: { "zh-CN": "", en: "" }, coordinate_system: {}, updated_at: "2026-02-01T00:00:00Z", published_at: "2026-02-01T00:00:00Z", views },
      { public_id: "draft-version", sop_public_id: "sop", schema_version: "1.0", version_number: 3, status: "draft", name: { "zh-CN": "草稿", en: "Draft" }, description: { "zh-CN": "", en: "" }, coordinate_system: {}, updated_at: "2026-03-01T00:00:00Z", views },
    ] }] });
    if (url.endsWith("/api/proxy/photo-sessions")) return response({ public_id: sessionID, code: "PS-0001", sku_id: skuID, sop_version_id: versionID, status: "in_progress", created_at: "2026-07-20T00:00:00Z" }, 201);
    if (url.endsWith("/api/proxy/assets/upload-url")) return response({ method: "PUT", upload_url: "https://uploads.example/photo.jpg", completion_token: "ticket", expires_in: 900, headers: { "content-type": "image/jpeg" } });
    if (url === "https://uploads.example/photo.jpg") {
      putCount += 1;
      if (failFirstPut && putCount === 1) return response(undefined, 503);
      return response(undefined, 204);
    }
    if (url.endsWith("/api/proxy/assets/complete")) {
      const body = JSON.parse(String(init?.body));
      return response({ public_id: `asset-${requests.length}`, sku_id: skuID, photo_session_id: sessionID, sop_view_id: body.sop_view_id, media_url: `/api/v1/assets/asset-${requests.length}/media`, review_status: "pending", captured_at: "2026-07-20T00:00:00Z" }, 201);
    }
    throw new Error(`Unexpected request ${method} ${url}`);
  });
  return requests;
}

describe("asset capture page", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    window.localStorage.clear();
    Object.defineProperty(URL, "createObjectURL", { configurable: true, value: vi.fn(() => `blob:preview-${Math.random()}`) });
    Object.defineProperty(URL, "revokeObjectURL", { configurable: true, value: vi.fn() });
  });

  it("filters active SKUs and defaults to the latest published SOP", async () => {
    const requests = installFetch();
    render(<CapturePage />, { wrapper: Providers });
    const sku = await chooseSKU();
    expect(screen.queryByRole("option", { name: /DRAFT/ })).not.toBeInTheDocument();
    expect(sku).toHaveValue(skuID);
    await waitFor(() => expect(requests.some((request) => request.url.includes("capture-sops?category_id=7"))).toBe(true));
    await waitFor(() => expect(screen.getByLabelText("选择 SOP 版本")).toHaveValue(versionID));
    expect(screen.queryByRole("option", { name: /草稿/ })).not.toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "手机壳拍摄 · v2" })).toBeInTheDocument();
  });

  it("creates one batch, uploads sequentially, and unlocks review after required views", async () => {
    const requests = installFetch();
    render(<CapturePage />, { wrapper: Providers });
    await chooseSKU();
    await screen.findByRole("heading", { name: "手机壳拍摄 · v2" });

    const front = new File(["front"], "front.png", { type: "image/png" });
    fireEvent.change(screen.getByLabelText("选择图片: 基准正面"), { target: { files: [front] } });
    expect(await screen.findByText("PS-0001")).toBeInTheDocument();
    await waitFor(() => expect(screen.getByRole("link", { name: /完成并查看素材/ })).toHaveAttribute("href", "/assets/review"));
    expect(screen.getByLabelText("选择 SKU")).toBeDisabled();
    expect(screen.getByLabelText("选择 SOP 版本")).toBeDisabled();

    const first = new File(["one"], "one.jpg", { type: "image/jpeg" });
    const second = new File(["two"], "two.jpg", { type: "image/jpeg" });
    fireEvent.change(screen.getByLabelText("选择图片: 标签细节"), { target: { files: [first, second] } });
    await waitFor(() => expect(requests.filter((request) => request.url.endsWith("/assets/complete"))).toHaveLength(3));
    expect(requests.filter((request) => request.url.endsWith("/photo-sessions"))).toHaveLength(1);
    const methods = requests.filter((request) => request.url.includes("assets/") || request.url.startsWith("https://uploads")).map((request) => request.method);
    expect(methods).toEqual(["POST", "PUT", "POST", "POST", "PUT", "POST", "POST", "PUT", "POST"]);
  });

  it("keeps a failed image and retries it", async () => {
    const requests = installFetch({ failFirstPut: true });
    render(<CapturePage />, { wrapper: Providers });
    await chooseSKU();
    await screen.findByRole("heading", { name: "手机壳拍摄 · v2" });
    fireEvent.change(screen.getByLabelText("选择图片: 基准正面"), { target: { files: [new File(["front"], "front.jpg", { type: "image/jpeg" })] } });

    expect((await screen.findAllByText("上传未完成。请检查网络后重试。")).length).toBeGreaterThan(0);
    expect(screen.queryByRole("link", { name: /完成并查看素材/ })).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "重试" }));
    await waitFor(() => expect(screen.getByRole("link", { name: /完成并查看素材/ })).toBeInTheDocument());
    expect(requests.filter((request) => request.url.endsWith("/photo-sessions"))).toHaveLength(1);
  });
});
