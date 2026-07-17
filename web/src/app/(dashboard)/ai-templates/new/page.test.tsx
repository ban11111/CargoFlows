import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { LanguageProvider } from "@/lib/i18n";
import NewAITemplatePage from "./page";

function Providers({ children }: { children: ReactNode }) {
  return <LanguageProvider><QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })}>{children}</QueryClientProvider></LanguageProvider>;
}

beforeEach(() => vi.restoreAllMocks());

describe("NewAITemplatePage", () => {
  it("blocks creation until bilingual names and a valid slot exist", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch");
    render(<NewAITemplatePage />, { wrapper: Providers });
    fireEvent.change(screen.getByLabelText("中文名称"), { target: { value: "Lazada 商品详情" } });
    fireEvent.click(screen.getByRole("button", { name: "创建草稿" }));
    expect(await screen.findByText("请输入英文名称")).toBeInTheDocument();
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("creates a typed draft, requires server validation, and then publishes", async () => {
    const version = { public_id: "version-1", version_number: 1, status: "draft", default_locale: "zh-CN", prompt_compiler_version: "v1", platform_prompt: "platform", published_at: null, archived_at: null, created_at: "2026-07-17T00:00:00Z", updated_at: "2026-07-17T00:00:00Z", slots: [] };
    const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation((_input) => {
      if (String(_input).includes("/validate")) return Promise.resolve(new Response(JSON.stringify({ code: "template_valid", issues: [] }), { status: 200 }));
      if (String(_input).includes("/publish")) return Promise.resolve(new Response(JSON.stringify({ ...version, status: "published", published_at: "2026-07-17T01:00:00Z" }), { status: 200 }));
      return Promise.resolve(new Response(JSON.stringify({ public_id: "template-1", name_zh: "Lazada 商品详情", name_en: "Lazada PDP", target_platform: "lazada", status: "active", created_at: "2026-07-17T00:00:00Z", updated_at: "2026-07-17T00:00:00Z", versions: [version] }), { status: 201 }));
    });
    render(<NewAITemplatePage />, { wrapper: Providers });

    fireEvent.change(screen.getByLabelText("中文名称"), { target: { value: "Lazada 商品详情" } });
    fireEvent.change(screen.getByLabelText("英文名称"), { target: { value: "Lazada PDP" } });
    fireEvent.change(screen.getByLabelText("平台基础要求"), { target: { value: "Mobile readable, no invented claims" } });
    fireEvent.click(screen.getByRole("button", { name: "添加标题" }));
    fireEvent.change(screen.getByLabelText("槽位中文名"), { target: { value: "商品标题" } });
    fireEvent.change(screen.getByLabelText("槽位英文名"), { target: { value: "Product title" } });
    fireEvent.change(screen.getByLabelText("槽位提示要求"), { target: { value: "Write a search-friendly title" } });
    fireEvent.click(screen.getByRole("button", { name: "创建草稿" }));

    expect(await screen.findByText("先运行服务端校验；只有完全通过后才可发布。")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "发布版本" })).toBeDisabled();
    const body = JSON.parse(String(fetchMock.mock.calls[0]?.[1]?.body));
    expect(body.slots[0]).toMatchObject({ kind: "title", slot_key: "title_1", sequence: 1, constraints: { min_length: 10, max_length: 120 } });

    fireEvent.click(screen.getByRole("button", { name: "运行发布校验" }));
    expect(await screen.findByText("校验通过，可以发布。")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "发布版本" })).toBeEnabled();
    fireEvent.click(screen.getByRole("button", { name: "发布版本" }));
    await waitFor(() => expect(screen.getByRole("button", { name: "已发布" })).toBeDisabled());
  });
});
