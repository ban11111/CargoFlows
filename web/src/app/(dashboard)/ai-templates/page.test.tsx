import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { LanguageProvider } from "@/lib/i18n";
import AITemplatesPage from "./page";

function Providers({ children }: { children: ReactNode }) {
  return <LanguageProvider><QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>{children}</QueryClientProvider></LanguageProvider>;
}

function jsonResponse(body: unknown) {
  return Promise.resolve(new Response(JSON.stringify(body), { status: 200, headers: { "content-type": "application/json" } }));
}

beforeEach(() => { localStorage.clear(); vi.restoreAllMocks(); vi.spyOn(window, "confirm").mockReturnValue(true); });

describe("AITemplatesPage", () => {
  it("groups ordered versions under their logical template", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(JSON.stringify({ data: [{ public_id: "template-1", name_zh: "Lazada 商品详情", name_en: "Lazada PDP", target_platform: "lazada", status: "active", created_at: "2026-07-17T00:00:00Z", updated_at: "2026-07-17T00:00:00Z", versions: [{ public_id: "v2", version_number: 2, status: "draft", default_locale: "zh-CN", prompt_compiler_version: "v1", platform_prompt: "prompt", published_at: null, archived_at: null, created_at: "2026-07-17T00:00:00Z", updated_at: "2026-07-17T00:00:00Z", slots: [] }, { public_id: "v1", version_number: 1, status: "published", default_locale: "zh-CN", prompt_compiler_version: "v1", platform_prompt: "prompt", published_at: "2026-07-17T00:00:00Z", archived_at: null, created_at: "2026-07-17T00:00:00Z", updated_at: "2026-07-17T00:00:00Z", slots: [] }] }] }), { status: 200 }));
    render(<AITemplatesPage />, { wrapper: Providers });
    expect(await screen.findByText("Lazada 商品详情")).toBeInTheDocument();
    expect(screen.getByText("V2")).toBeInTheDocument();
    expect(screen.getByText("V1")).toBeInTheDocument();
    expect(screen.getByText("草稿")).toBeInTheDocument();
    expect(screen.getByText("已启用")).toBeInTheDocument();
  });

  it("lets an archived version be re-enabled without creating a new version", async () => {
    const versionID = "33333333-3333-4333-8333-333333333333";
    const template = { public_id: "44444444-4444-4444-8444-444444444444", name_zh: "Lazada 商品详情", name_en: "Lazada PDP", target_platform: "lazada", status: "archived", created_at: "2026-07-17T00:00:00Z", updated_at: "2026-07-17T00:00:00Z", versions: [{ public_id: versionID, version_number: 1, status: "archived", default_locale: "zh-CN", prompt_compiler_version: "v1", platform_prompt: "prompt", published_at: "2026-07-17T00:00:00Z", archived_at: "2026-07-18T00:00:00Z", created_at: "2026-07-17T00:00:00Z", updated_at: "2026-07-18T00:00:00Z", slots: [] }] };
    let enabled = false;
    const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation((input, init) => {
      const url = String(input);
      if (url.endsWith(`/ai-content-template-versions/${versionID}/restore`) && init?.method === "POST") {
        enabled = true;
        return jsonResponse({ ...template.versions[0], status: "published", archived_at: null });
      }
      return jsonResponse({ data: [{ ...template, status: enabled ? "active" : "archived", versions: [{ ...template.versions[0], status: enabled ? "published" : "archived", archived_at: enabled ? null : template.versions[0].archived_at }] }] });
    });

    render(<AITemplatesPage />, { wrapper: Providers });

    expect(await screen.findByText("已停用")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "重新启用 V1" }));
    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith(`/api/proxy/ai-content-template-versions/${versionID}/restore`, expect.objectContaining({ method: "POST" })));
    expect(await screen.findByRole("button", { name: "停用版本 V1" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "重新启用 V1" })).not.toBeInTheDocument();
  });
});
