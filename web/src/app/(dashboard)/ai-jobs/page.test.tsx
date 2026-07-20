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
    vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(JSON.stringify({ data: [{ public_id: "job-real", sku_id: "11111111-1111-4111-8111-111111111111", template_version_id: "version-1", target_platform: "lazada", locale: "zh-CN", status: "queued", snapshot_schema: "cargoflows_product_generation_v1", input_snapshot: { sku: { public_id: "11111111-1111-4111-8111-111111111111", code: "CF-REAL-001" }, selected_assets: [{ public_id: "22222222-2222-4222-8222-222222222222" }], template: { selected_slots: [{ slot_key: "hero" }] } }, created_by: { public_id: "33333333-3333-4333-8333-333333333333", name: "Operator One", email: "operator@example.test" }, created_by_snapshot: { public_id: "33333333-3333-4333-8333-333333333333", name: "Operator One", email: "operator@example.test" }, model_snapshot: { text_model: "gpt-5.6-terra", image_api_mode: "images", image_responses_model: "gpt-5.6", image_generation_model: "gpt-image-2" }, started_at: null, completed_at: null, cancelled_at: null, created_at: "2026-07-17T00:00:00Z", updated_at: "2026-07-17T00:00:00Z", items: [] }] }), { status: 200 }));
    render(<AIJobsPage />, { wrapper: Providers });
    const sku = await screen.findByRole("link", { name: "CF-REAL-001" });
    expect(sku).toHaveAttribute("href", "/ai-jobs/job-real");
    expect(screen.getByText("Operator One")).toBeInTheDocument();
    expect(screen.getByText("operator@example.test")).toBeInTheDocument();
    expect(screen.getByText("gpt-5.6-terra")).toBeInTheDocument();
    expect(screen.getByText(/gpt-image-2 · images/)).toBeInTheDocument();
    expect(screen.queryByText("CF-CAP-CRM-F")).not.toBeInTheDocument();
  });
});
