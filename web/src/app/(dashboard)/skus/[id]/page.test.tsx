import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { LanguageProvider } from "@/lib/i18n";
import SkuDetailPage from "./page";

vi.mock("next/navigation", () => ({ useParams: () => ({ id: "11111111-1111-4111-8111-111111111111" }), useRouter: () => ({ push: vi.fn() }) }));

const sku = {
  public_id: "11111111-1111-4111-8111-111111111111", code: "CASE-BLK", color: "黑色", size: "标准", compatible_device_model: "iPhone 17 Pro", barcode: "", stock: 8, low_stock_threshold: 2,
  platform_title: "星轨保护壳", selling_points: "轻薄", status: "active" as const, tags: [{ name: "主推" }],
  product: { category_id: 1, name: "星轨保护壳 黑色", brand: "Aster", category: "配件", category_record: { name: "配件", name_en: "Accessories" } },
};

function Providers({ children }: { children: ReactNode }) {
  return <LanguageProvider><QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })}>{children}</QueryClientProvider></LanguageProvider>;
}

function response(body: unknown, status = 200) {
  return Promise.resolve(new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } }));
}

beforeEach(() => {
  localStorage.clear();
  vi.restoreAllMocks();
  vi.spyOn(window, "confirm").mockReturnValue(true);
});

describe("SKU lifecycle controls", () => {
  it("disables and re-enables a SKU with visible status feedback", async () => {
    let currentSKU = { ...sku, status: "active" as "active" | "disabled" };
    const patches: Array<Record<string, unknown>> = [];
    vi.spyOn(globalThis, "fetch").mockImplementation((input, init) => {
      if (init?.method === "PATCH") {
        const body = JSON.parse(String(init.body)) as Record<string, unknown>;
        patches.push(body);
        currentSKU = { ...currentSKU, status: body.status as "active" | "disabled" };
      }
      return response(currentSKU);
    });

    render(<SkuDetailPage />, { wrapper: Providers });

    expect(await screen.findByText("星轨保护壳 黑色")).toBeInTheDocument();
    expect(screen.getByText("启用")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "停用 SKU" }));

    await waitFor(() => expect(patches.at(-1)?.status).toBe("disabled"));
    expect(await screen.findByRole("status")).toHaveTextContent("SKU 已停用");
    expect(await screen.findByRole("button", { name: "启用 SKU" })).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "启用 SKU" }));
    await waitFor(() => expect(patches.at(-1)?.status).toBe("active"));
    expect(await screen.findByRole("status")).toHaveTextContent("SKU 已启用");
    expect(patches[0]).toMatchObject({ category_id: 1, product_name: "星轨保护壳 黑色", code: "CASE-BLK", status: "disabled", tags: ["主推"] });
  });
});
