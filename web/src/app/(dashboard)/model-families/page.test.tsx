import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { LanguageProvider } from "@/lib/i18n";
import ModelFamiliesPage from "./page";

const family = {
  public_id: "11111111-1111-4111-8111-111111111111", brand: "Aster", name_zh: "星轨保护壳", name_en: "Orbit Case", model_code: "AST-ORBIT",
  common_structure: { schema: "model_family_common_structure_v1", invariants: ["相机开孔位置一致"] }, variation_dimensions: ["color", "material"], status: "active",
  created_at: "2026-07-20T00:00:00Z", updated_at: "2026-07-20T00:00:00Z",
};

function Providers({ children }: { children: ReactNode }) {
  return <LanguageProvider><QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })}>{children}</QueryClientProvider></LanguageProvider>;
}

function response(body: unknown, status = 200) { return Promise.resolve(new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } })); }

beforeEach(() => { localStorage.clear(); vi.restoreAllMocks(); });

describe("model family list", () => {
  it("renders existing families and exposes the detail route", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation((input) => String(input).endsWith("/auth/me") ? response({ role: "admin" }) : response({ data: [family] }));
    render(<ModelFamiliesPage />, { wrapper: Providers });

    expect(await screen.findByText("星轨保护壳")).toBeInTheDocument();
    expect(screen.getByText("颜色")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "查看型号组: 星轨保护壳" })).toHaveAttribute("href", `/model-families/${family.public_id}`);
  });

  it("creates a family with normalized invariants and selected dimensions", async () => {
    let posted: Record<string, unknown> | undefined;
    vi.spyOn(globalThis, "fetch").mockImplementation((input, init) => {
      const url = String(input);
      if (url.endsWith("/auth/me")) return response({ role: "admin" });
      if (init?.method === "POST") { posted = JSON.parse(String(init.body)); return response(family, 201); }
      return response({ data: [] });
    });
    render(<ModelFamiliesPage />, { wrapper: Providers });
    await screen.findByText("还没有型号组。创建第一个型号组来组织相关 SKU。");
    fireEvent.click(screen.getByRole("button", { name: "新建型号组" }));
    fireEvent.change(screen.getByLabelText("品牌"), { target: { value: "Aster" } });
    fireEvent.change(screen.getByLabelText("系列代码"), { target: { value: "AST-ORBIT" } });
    fireEvent.change(screen.getByLabelText("中文系列名称"), { target: { value: "星轨保护壳" } });
    fireEvent.change(screen.getByLabelText("英文系列名称"), { target: { value: "Orbit Case" } });
    fireEvent.change(screen.getByLabelText("共同结构（每行一项）"), { target: { value: " 相机开孔位置一致 \n\n 按键布局一致 " } });
    fireEvent.click(screen.getByRole("button", { name: "颜色" }));
    fireEvent.click(screen.getByRole("button", { name: "创建型号组" }));

    await waitFor(() => expect(posted).toBeDefined());
    expect(posted).toMatchObject({
      brand: "Aster", name_zh: "星轨保护壳", name_en: "Orbit Case", model_code: "AST-ORBIT", variation_dimensions: ["color"],
      common_structure: { schema: "model_family_common_structure_v1", invariants: ["相机开孔位置一致", "按键布局一致"] },
    });
  });
});
