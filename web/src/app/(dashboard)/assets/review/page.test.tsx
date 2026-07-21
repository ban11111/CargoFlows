import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { LanguageProvider } from "@/lib/i18n";
import AssetReviewPage from "./page";

let searchParams = new URLSearchParams();
const replace = vi.fn();
vi.mock("next/navigation", () => ({ useRouter: () => ({ replace }), useSearchParams: () => searchParams }));

function Providers({ children }: { children: ReactNode }) {
  return <LanguageProvider><QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })}>{children}</QueryClientProvider></LanguageProvider>;
}

function response(body: unknown, status = 200) {
  return Promise.resolve(new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } }));
}

const category = { id: 1, name: "手机壳", name_en: "Phone cases", is_system: true };
const queueSKU = (index: number) => ({
  public_id: `11111111-1111-4111-8111-${String(index).padStart(12, "0")}`,
  code: `CASE-${String(index).padStart(2, "0")}`,
  product_name: `保护壳 ${index}`,
  category,
  tags: [{ name: "主推" }],
  counts: { pending: index === 1 ? 5 : 0, approved: 2, rejected: 1, total: index === 1 ? 8 : 3 },
  latest_asset_at: "2026-07-21T10:00:00Z",
  latest_pending_at: index === 1 ? "2026-07-21T10:00:00Z" : null,
  cover_asset: { public_id: `asset-${index}`, media_url: `/api/v1/assets/asset-${index}/media`, review_status: index === 1 ? "pending" : "approved", origin_type: "captured" },
});

beforeEach(() => {
  localStorage.clear();
  searchParams = new URLSearchParams();
  replace.mockReset();
  vi.restoreAllMocks();
});

describe("AssetReviewPage", () => {
  it("renders only the server page as a compact SKU queue and preserves filters in detail links", async () => {
    const page = Array.from({ length: 40 }, (_, index) => queueSKU(index + 1));
    const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation((input) => {
      const url = String(input);
      if (url.endsWith("/categories")) return response({ data: [category] });
      if (url.includes("/assets/review/skus")) return response({ data: page, pagination: { page: 1, page_size: 40, total: 41, total_pages: 2 } });
      return response({ data: [] });
    });
    render(<AssetReviewPage />, { wrapper: Providers });

    expect(await screen.findAllByText("CASE-01")).not.toHaveLength(0);
    expect(screen.queryByText("CASE-41")).not.toBeInTheDocument();
    expect(screen.getByText("41 个 SKU")).toBeInTheDocument();
    expect(screen.getAllByRole("link", { name: /CASE-01/ })[0]).toHaveAttribute("href", expect.stringContaining("/assets/review/11111111-1111-4111-8111-000000000001?return="));
    expect(fetchMock.mock.calls.some(([url]) => String(url).includes("page_size=40"))).toBe(true);

    fireEvent.change(screen.getByLabelText("状态"), { target: { value: "pending" } });
    expect(replace).toHaveBeenCalledWith("/assets/review?status=pending");
  });

  it("moves style grants into an independent URL-driven tab", async () => {
    searchParams = new URLSearchParams("tab=styles");
    const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation((input) => String(input).includes("style-reference-grants") ? response({ data: [] }) : response({ data: [category] }));
    render(<AssetReviewPage />, { wrapper: Providers });

    expect(await screen.findByText("暂无风格授权。请先在已通过素材的详情中创建授权。")).toBeInTheDocument();
    expect(fetchMock.mock.calls.some(([url]) => String(url).includes("/assets/review/skus"))).toBe(false);
  });
});
