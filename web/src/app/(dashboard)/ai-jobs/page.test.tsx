import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";

import { LanguageProvider } from "@/lib/i18n";
import AIJobsPage from "./page";

function Providers({ children }: { children: ReactNode }) {
  return <LanguageProvider><QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>{children}</QueryClientProvider></LanguageProvider>;
}

describe("AIJobsPage", () => {
  it("renders jobs from the API and links to the real job detail", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(JSON.stringify({ data: [{ public_id: "job-real", sku_id: 11, template_version_id: "version-1", target_platform: "lazada", locale: "zh-CN", status: "queued", snapshot_schema: "cargoflow_product_generation_v1", input_snapshot: { sku: { code: "CF-REAL-001" }, selected_assets: [{ id: 31 }], template: { selected_slots: [{ slot_key: "hero" }] } }, started_at: null, completed_at: null, cancelled_at: null, created_at: "2026-07-17T00:00:00Z", updated_at: "2026-07-17T00:00:00Z", items: [] }] }), { status: 200 }));
    render(<AIJobsPage />, { wrapper: Providers });
    const sku = await screen.findByRole("link", { name: "CF-REAL-001" });
    expect(sku).toHaveAttribute("href", "/ai-jobs/job-real");
    expect(screen.queryByText("CF-CAP-CRM-F")).not.toBeInTheDocument();
  });
});
