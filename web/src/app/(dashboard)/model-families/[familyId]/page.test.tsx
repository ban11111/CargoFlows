import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { LanguageProvider } from "@/lib/i18n";
import ModelFamilyDetailPage from "./page";

vi.mock("next/navigation", () => ({ useParams: () => ({ familyId: "11111111-1111-4111-8111-111111111111" }) }));

const skuID = "22222222-2222-4222-8222-222222222222";
const memberID = "33333333-3333-4333-8333-333333333333";
const family = {
  public_id: "11111111-1111-4111-8111-111111111111", brand: "Aster", name_zh: "星轨保护壳", name_en: "Orbit Case", model_code: "AST-ORBIT",
  common_structure: { schema: "model_family_common_structure_v1", invariants: ["相机开孔位置一致"] }, variation_dimensions: ["color"], status: "active",
  created_at: "2026-07-20T00:00:00Z", updated_at: "2026-07-20T00:00:00Z", members: [{ public_id: memberID, sku_id: skuID, removed_at: null, created_at: "2026-07-20T00:00:00Z" }],
};
const sku = { public_id: skuID, code: "CASE-BLK", color: "黑色", size: "标准", barcode: "", stock: 1, low_stock_threshold: 0, platform_title: "", selling_points: "", status: "active", created_at: "", updated_at: "", product: { category_id: 1, name: "星轨保护壳 黑色", brand: "Aster", category: "配件", description: "", category_record: { id: 1, name: "配件", name_en: "Accessories" } }, tags: [] };

function Providers({ children }: { children: ReactNode }) {
  return <LanguageProvider><QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })}>{children}</QueryClientProvider></LanguageProvider>;
}
function response(body: unknown, status = 200) { return Promise.resolve(new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } })); }

beforeEach(() => { localStorage.clear(); vi.restoreAllMocks(); vi.spyOn(window, "confirm").mockReturnValue(true); });

describe("model family detail", () => {
  it("lets an operator manage members while keeping admin lifecycle actions hidden", async () => {
    const requests: Array<{ url: string; method?: string }> = [];
    vi.spyOn(globalThis, "fetch").mockImplementation((input, init) => {
      const url = String(input); requests.push({ url, method: init?.method });
      if (url.endsWith("/auth/me")) return response({ role: "operator" });
      if (url.endsWith("/skus")) return response({ data: [sku] });
      if (url.includes("/variant-identity")) return response({ code: "not_found" }, 404);
      if (init?.method === "DELETE") return Promise.resolve(new Response(null, { status: 204 }));
      return response(family);
    });
    render(<ModelFamilyDetailPage />, { wrapper: Providers });

    expect(await screen.findByText("星轨保护壳 黑色")).toBeInTheDocument();
    expect(await screen.findByText("变体身份: 未发布")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "编辑资料" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "归档型号组" })).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "移除: CASE-BLK" }));
    await waitFor(() => expect(requests.some((request) => request.method === "DELETE" && request.url.includes(memberID))).toBe(true));
  });

  it("shows lifecycle actions to administrators", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation((input) => {
      const url = String(input);
      if (url.endsWith("/auth/me")) return response({ role: "admin" });
      if (url.endsWith("/skus")) return response({ data: [sku] });
      if (url.includes("/variant-identity")) return response({ code: "not_found" }, 404);
      return response(family);
    });
    render(<ModelFamilyDetailPage />, { wrapper: Providers });
    expect(await screen.findByRole("button", { name: "编辑资料" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "归档型号组" })).toBeInTheDocument();
  });
});
