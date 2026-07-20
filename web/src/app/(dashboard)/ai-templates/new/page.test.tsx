import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { LanguageProvider } from "@/lib/i18n";
import { LanguageToggle } from "@/components/language-toggle";
import NewAITemplatePage from "./page";

function Providers({ children }: { children: ReactNode }) {
  return <LanguageProvider><LanguageToggle /><QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })}>{children}</QueryClientProvider></LanguageProvider>;
}

beforeEach(() => {
  localStorage.clear();
  vi.restoreAllMocks();
});

describe("NewAITemplatePage", () => {
  it("enables all twenty styles and defaults new image slots to white background", () => {
    render(<NewAITemplatePage />, { wrapper: Providers });
    fireEvent.click(screen.getByRole("button", { name: "添加图片" }));

    expect(screen.getByText("已启用 20/20；任务创建时选择一种。")).toBeInTheDocument();
    expect(screen.getAllByRole("checkbox").filter((control) => (control as HTMLInputElement).checked)).toHaveLength(20);
    expect(screen.getByLabelText("默认图片风格")).toHaveValue("clean_white_background");
    expect(screen.getAllByText("白底棚拍")).toHaveLength(2);
    expect(screen.getByText("纯白背景、柔和落地阴影，适合平台主图。")).toBeInTheDocument();
  });

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
    let publishCount = 0;
    let validateCount = 0;
    let resolveFirstValidation: (response: Response) => void = () => undefined;
    const firstValidation = new Promise<Response>((resolve) => { resolveFirstValidation = resolve; });
    const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation((_input, init) => {
      if (String(_input).includes("/validate")) {
        validateCount += 1;
        return validateCount === 1 ? firstValidation : Promise.resolve(new Response(JSON.stringify({ code: "template_valid", issues: [] }), { status: 200 }));
      }
      if (String(_input).includes("/publish")) {
        publishCount += 1;
        return publishCount === 1
          ? Promise.resolve(new Response(JSON.stringify({ code: "template_validation_failed", issues: [{ code: "slot_key_duplicate", path: "slots[1].slot_key", message: "Slot key must be unique within a version." }, { code: "prompt_secret_forbidden", path: "platform_prompt", message: "Prompt content appears to contain a secret." }, { code: "template_variable_unknown", path: "slots[0].prompt_fragment", message: "Template variable is not supported." }] }), { status: 422 }))
          : Promise.resolve(new Response(JSON.stringify({ ...version, status: "published", published_at: "2026-07-17T01:00:00Z" }), { status: 200 }));
      }
      if (init?.method === "PATCH") return Promise.resolve(new Response(JSON.stringify(version), { status: 200 }));
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

    fireEvent.change(screen.getByLabelText("英文名称"), { target: { value: "Updated Lazada PDP" } });
    fireEvent.click(screen.getByRole("button", { name: "运行发布校验" }));
    fireEvent.change(screen.getByLabelText("英文名称"), { target: { value: "Edited while validating" } });
    resolveFirstValidation(new Response(JSON.stringify({ code: "template_valid", issues: [] }), { status: 200 }));
    await waitFor(() => expect(screen.getByRole("button", { name: "运行发布校验" })).toBeEnabled());
    await waitFor(() => expect(screen.getByRole("button", { name: "发布版本" })).toBeDisabled());
    fireEvent.click(screen.getByRole("button", { name: "运行发布校验" }));
    expect(await screen.findByText("校验通过，可以发布。")).toBeInTheDocument();
    const patchCall = fetchMock.mock.calls.find(([, init]) => init?.method === "PATCH");
    expect(JSON.parse(String(patchCall?.[1]?.body))).toMatchObject({ name_en: "Updated Lazada PDP" });
    expect(screen.getByRole("button", { name: "发布版本" })).toBeEnabled();

    fireEvent.change(screen.getByLabelText("平台基础要求"), { target: { value: "Updated platform direction" } });
    await waitFor(() => expect(screen.getByRole("button", { name: "发布版本" })).toBeDisabled());
    fireEvent.click(screen.getByRole("button", { name: "运行发布校验" }));
    await waitFor(() => expect(screen.getByRole("button", { name: "发布版本" })).toBeEnabled());
    fireEvent.click(screen.getByRole("button", { name: "发布版本" }));
    expect(await screen.findByText("slots[1].slot_key")).toBeInTheDocument();
    expect(screen.getByText(/槽位键在当前版本中必须唯一/)).toBeInTheDocument();
    expect(screen.getByText(/提示内容疑似包含密钥/)).toBeInTheDocument();
    expect(screen.getByText(/不支持的模板变量/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "发布版本" })).toBeDisabled();
    fireEvent.click(screen.getByRole("button", { name: "语言" }));
    expect(screen.getByText(/Slot key must be unique within this version/)).toBeInTheDocument();
    expect(screen.getByText(/Prompt content appears to contain a secret/)).toBeInTheDocument();
    expect(screen.getByText(/unsupported template variable/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Language" }));

    fireEvent.click(screen.getByRole("button", { name: "运行发布校验" }));
    await waitFor(() => expect(screen.getByRole("button", { name: "发布版本" })).toBeEnabled());
    fireEvent.click(screen.getByRole("button", { name: "发布版本" }));
    await waitFor(() => expect(screen.getByRole("button", { name: "已发布" })).toBeDisabled());
  });

  it("retranslates active client validation and allocates non-colliding slot keys", async () => {
    vi.spyOn(globalThis, "fetch");
    render(<NewAITemplatePage />, { wrapper: Providers });
    fireEvent.click(screen.getByRole("button", { name: "创建草稿" }));
    expect(await screen.findByText("请输入中文名称")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "语言" }));
    expect(screen.getByText("Enter a Chinese name")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Add title" }));
    fireEvent.click(screen.getByRole("button", { name: "Add title" }));
    const removeButtons = screen.getAllByRole("button", { name: "Remove slot" });
    fireEvent.click(removeButtons[0]);
    fireEvent.click(screen.getByRole("button", { name: "Add title" }));
    expect(screen.getAllByLabelText("Slot key").map((input) => (input as HTMLInputElement).value).sort()).toEqual(["title_1", "title_2"]);
  });

  it("submits reordered multi-slot drafts with contiguous one-based sequences", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(JSON.stringify({ public_id: "template-2", name_zh: "套图", name_en: "Suite", target_platform: "lazada", status: "active", created_at: "2026-07-17T00:00:00Z", updated_at: "2026-07-17T00:00:00Z", versions: [] }), { status: 201 }));
    render(<NewAITemplatePage />, { wrapper: Providers });
    fireEvent.change(screen.getByLabelText("中文名称"), { target: { value: "套图" } });
    fireEvent.change(screen.getByLabelText("英文名称"), { target: { value: "Suite" } });
    fireEvent.change(screen.getByLabelText("平台基础要求"), { target: { value: "Platform direction" } });
    fireEvent.click(screen.getByRole("button", { name: "添加标题" }));
    fireEvent.click(screen.getByRole("button", { name: "添加标题" }));
    screen.getAllByLabelText("槽位中文名").forEach((input, index) => fireEvent.change(input, { target: { value: `标题 ${index + 1}` } }));
    screen.getAllByLabelText("槽位英文名").forEach((input, index) => fireEvent.change(input, { target: { value: `Title ${index + 1}` } }));
    screen.getAllByLabelText("槽位提示要求").forEach((input, index) => fireEvent.change(input, { target: { value: `Write title ${index + 1}` } }));
    fireEvent.click(screen.getAllByRole("button", { name: "上移槽位" })[1]);
    fireEvent.click(screen.getByRole("button", { name: "创建草稿" }));
    await waitFor(() => expect(fetchMock).toHaveBeenCalled());
    const body = JSON.parse(String(fetchMock.mock.calls[0]?.[1]?.body));
    expect(body.slots.map((slot: { slot_key: string; sequence: number }) => [slot.slot_key, slot.sequence])).toEqual([["title_2", 1], ["title_1", 2]]);
  });
});
