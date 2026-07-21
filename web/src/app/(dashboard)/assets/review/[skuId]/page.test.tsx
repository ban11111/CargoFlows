import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { LanguageProvider } from "@/lib/i18n";
import SKUAssetReviewPage from "./page";

const skuID = "11111111-1111-4111-8111-111111111111";
vi.mock("next/navigation", () => ({ useParams: () => ({ skuId: skuID }), useSearchParams: () => new URLSearchParams("return=%2Fassets%2Freview%3Fstatus%3Dpending") }));

function Providers({ children }: { children: ReactNode }) {
  return <LanguageProvider><QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })}>{children}</QueryClientProvider></LanguageProvider>;
}

function response(body: unknown, status = 200) {
  return Promise.resolve(new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } }));
}

const makeAsset = (id: string, status: "pending" | "approved" | "rejected") => ({ public_id: id, sku_id: skuID, media_url: `/api/v1/assets/${id}/media`, review_status: status, captured_at: "2026-07-21T10:00:00Z", sop_view_key: id, sop_view_name: { "zh-CN": id === "asset-1" ? "正面" : "背面", en: id === "asset-1" ? "Front" : "Back" }, photo_session_code: "PS-1", origin_type: "captured", source_summary: {} });

beforeEach(() => {
  localStorage.clear();
  vi.restoreAllMocks();
});

describe("SKUAssetReviewPage", () => {
  it("lazy-loads status groups, advances after review, and restores pending on undo", async () => {
    let pending = [makeAsset("asset-1", "pending"), makeAsset("asset-2", "pending")];
    let approved: ReturnType<typeof makeAsset>[] = [];
    const requests: string[] = [];
    const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation((input, init) => {
      const url = String(input);
      requests.push(url);
      if (url.includes(`/assets/review/skus/${skuID}`)) return response({ public_id: skuID, code: "CASE-01", product_name: "保护壳", category: { id: 1, name: "手机壳", name_en: "Phone cases", is_system: true }, tags: [], counts: { pending: pending.length, approved: approved.length, rejected: 0, total: pending.length + approved.length } });
      if (url.includes("/assets/review?") && url.includes("status=pending")) return response({ data: pending, pagination: { page: 1, page_size: 48, total: pending.length, total_pages: pending.length ? 1 : 0 } });
      if (url.includes("/assets/review?") && url.includes("status=approved")) return response({ data: approved, pagination: { page: 1, page_size: 48, total: approved.length, total_pages: approved.length ? 1 : 0 } });
      if (init?.method === "PATCH") {
        const id = url.split("/").at(-2)!;
        const body = JSON.parse(String(init.body)) as { status: "pending" | "approved" | "rejected" };
        if (body.status === "approved") {
          pending = pending.filter((asset) => asset.public_id !== id);
          approved = [...approved, makeAsset(id, "approved")];
        } else if (body.status === "pending") {
          approved = approved.filter((asset) => asset.public_id !== id);
          pending = [makeAsset(id, "pending"), ...pending];
        }
        return response({ public_id: id, review_status: body.status });
      }
      return response({ data: [], pagination: { page: 1, page_size: 48, total: 0, total_pages: 0 } });
    });
    render(<SKUAssetReviewPage />, { wrapper: Providers });

    expect(await screen.findByText("CASE-01")).toBeInTheDocument();
    expect(await screen.findByRole("button", { name: "正面 · 待审核" })).toHaveAttribute("aria-pressed", "true");
    expect(requests.some((url) => url.includes("status=approved"))).toBe(false);

    fireEvent.keyDown(window, { key: "ArrowRight" });
    expect(screen.getByRole("button", { name: "背面 · 待审核" })).toHaveAttribute("aria-pressed", "true");
    fireEvent.click(screen.getByRole("button", { name: "批准" }));
    await waitFor(() => expect(fetchMock.mock.calls.some(([url, init]) => String(url).includes("asset-2/review") && init?.method === "PATCH")).toBe(true));
    expect(await screen.findByText("已批准素材")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "正面 · 待审核" })).toHaveAttribute("aria-pressed", "true");

    fireEvent.click(screen.getByRole("button", { name: "撤销" }));
    await waitFor(() => {
      const undoCall = fetchMock.mock.calls.find(([url, init]) => String(url).includes("asset-2/review") && init?.method === "PATCH" && String(init.body).includes('"status":"pending"'));
      expect(undoCall).toBeTruthy();
    });
    expect(await screen.findByRole("button", { name: "背面 · 待审核" })).toHaveAttribute("aria-pressed", "true");

    fireEvent.click(screen.getByRole("button", { name: /已通过/ }));
    await waitFor(() => expect(requests.some((url) => url.includes("status=approved"))).toBe(true));
  });
});
