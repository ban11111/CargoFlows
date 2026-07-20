import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { LanguageProvider } from "@/lib/i18n";

import SkusPage from "./page";

const activeSKU = {
  public_id: "11111111-1111-4111-8111-111111111111",
  code: "CASE-BLK",
  color: "黑色",
  size: "标准",
  barcode: "690000000001",
  stock: 12,
  low_stock_threshold: 3,
  platform_title: "星轨保护壳",
  selling_points: "防摔",
  status: "active" as const,
  product: { category_id: 1, name: "星轨保护壳", brand: "DigiHive", category: "手机配件", category_record: { name: "手机配件", name_en: "Phone accessories" } },
  tags: [{ name: "主推" }],
};

const disabledSKU = {
  ...activeSKU,
  public_id: "22222222-2222-4222-8222-222222222222",
  code: "CASE-WHT",
  color: "白色",
  status: "disabled" as const,
};

function Providers({ children }: { children: ReactNode }) {
  return <LanguageProvider><QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>{children}</QueryClientProvider></LanguageProvider>;
}

function jsonResponse(body: unknown, status = 200) {
  return Promise.resolve(new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } }));
}

beforeEach(() => {
  localStorage.clear();
  vi.restoreAllMocks();
  vi.spyOn(window, "confirm").mockReturnValue(true);
});

describe("SKU list lifecycle actions", () => {
  it("disables and enables SKUs directly from their table rows", async () => {
    let skus = [activeSKU, disabledSKU] as Array<typeof activeSKU | typeof disabledSKU>;
    const patches: Array<Record<string, unknown>> = [];
    const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation((input, init) => {
      const url = String(input);
      if (url.endsWith("/categories")) return jsonResponse({ data: [] });
      if (init?.method === "PATCH") {
        const publicID = url.split("/").at(-1);
        const body = JSON.parse(String(init.body)) as Record<string, unknown>;
        patches.push(body);
        const current = skus.find((sku) => sku.public_id === publicID)!;
        const updated = { ...current, status: body.status as "active" | "disabled" };
        skus = skus.map((sku) => sku.public_id === publicID ? updated : sku);
        return jsonResponse(updated);
      }
      return jsonResponse({ data: skus });
    });

    render(<SkusPage />, { wrapper: Providers });

    fireEvent.click(await screen.findByRole("button", { name: "停用 SKU CASE-BLK" }));
    expect(window.confirm).toHaveBeenCalled();
    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith(`/api/proxy/skus/${activeSKU.public_id}`, expect.objectContaining({ method: "PATCH" })));
    expect(patches[0]).toMatchObject({ category_id: 1, product_name: "星轨保护壳", barcode: "690000000001", status: "disabled", tags: ["主推"] });
    expect(await screen.findByRole("button", { name: "启用 SKU CASE-BLK" })).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "启用 SKU CASE-WHT" }));
    await waitFor(() => expect(patches.at(-1)?.status).toBe("active"));
    expect(await screen.findByRole("button", { name: "停用 SKU CASE-WHT" })).toBeInTheDocument();
  });
});
