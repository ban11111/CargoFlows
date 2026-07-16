import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { LanguageProvider } from "@/lib/i18n";

import NewSOPPage from "./page";

vi.mock("next/navigation", () => ({ useRouter: () => ({ push: vi.fn() }) }));

function Providers({ children }: { children: ReactNode }) {
  return <LanguageProvider><QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>{children}</QueryClientProvider></LanguageProvider>;
}

function jsonResponse(body: unknown, status = 200) {
  return Promise.resolve(new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } }));
}

beforeEach(() => { localStorage.clear(); vi.restoreAllMocks(); });

describe("new SOP category loading", () => {
  it("shows a visible category loading state", () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(() => new Promise(() => undefined));
    render(<NewSOPPage />, { wrapper: Providers });
    expect(screen.getByText("正在载入分类…")).toBeInTheDocument();
  });

  it("shows a category error with a 44px retry action", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(() => jsonResponse({ code: "failed" }, 500));
    render(<NewSOPPage />, { wrapper: Providers });

    expect(await screen.findByText("无法载入商品分类，请重试。")).toBeInTheDocument();
    const retry = screen.getByRole("button", { name: "重试载入商品分类" });
    expect(retry).toHaveClass("min-h-11");
    fireEvent.click(retry);
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2));
  });

  it("distinguishes an empty category list from a request failure", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(() => jsonResponse({ data: [] }));
    render(<NewSOPPage />, { wrapper: Providers });
    expect(await screen.findByText("暂无可用分类，请先创建商品分类。")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "重试载入商品分类" })).not.toBeInTheDocument();
  });
});
