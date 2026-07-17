import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";

import { LanguageProvider } from "@/lib/i18n";
import AITemplatesPage from "./page";

function Providers({ children }: { children: ReactNode }) {
  return <LanguageProvider><QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>{children}</QueryClientProvider></LanguageProvider>;
}

describe("AITemplatesPage", () => {
  it("groups ordered versions under their logical template", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(JSON.stringify({ data: [{ public_id: "template-1", name_zh: "Lazada 商品详情", name_en: "Lazada PDP", target_platform: "lazada", status: "active", created_at: "2026-07-17T00:00:00Z", updated_at: "2026-07-17T00:00:00Z", versions: [{ public_id: "v2", version_number: 2, status: "draft", default_locale: "zh-CN", prompt_compiler_version: "v1", platform_prompt: "prompt", published_at: null, archived_at: null, created_at: "2026-07-17T00:00:00Z", updated_at: "2026-07-17T00:00:00Z", slots: [] }, { public_id: "v1", version_number: 1, status: "published", default_locale: "zh-CN", prompt_compiler_version: "v1", platform_prompt: "prompt", published_at: "2026-07-17T00:00:00Z", archived_at: null, created_at: "2026-07-17T00:00:00Z", updated_at: "2026-07-17T00:00:00Z", slots: [] }] }] }), { status: 200 }));
    render(<AITemplatesPage />, { wrapper: Providers });
    expect(await screen.findByText("Lazada 商品详情")).toBeInTheDocument();
    expect(screen.getByText("V2")).toBeInTheDocument();
    expect(screen.getByText("V1")).toBeInTheDocument();
    expect(screen.getByText("草稿")).toBeInTheDocument();
    expect(screen.getByText("已发布")).toBeInTheDocument();
  });
});
