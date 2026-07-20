import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { AppShell } from "@/components/app-shell";
import { LanguageToggle } from "@/components/language-toggle";
import { LanguageProvider } from "@/lib/i18n";

import OpenAISettingsPage from "./page";

const unconfigured = {
  provider: "openai",
  status: "unconfigured",
  key_fingerprint: "",
  verified_at: null,
  image_capability_verified_at: null,
  last_used_at: null,
};

const active = {
  provider: "openai",
  status: "active",
  key_fingerprint: "ABCD1234",
  verified_at: "2026-07-17T10:00:00Z",
  image_capability_verified_at: "2026-07-17T10:00:01Z",
  last_used_at: null,
};

let queryClient: QueryClient;

function Providers({ children }: { children: ReactNode }) {
  return (
    <LanguageProvider>
      <LanguageToggle />
      <QueryClientProvider
        client={queryClient}
      >
        {children}
      </QueryClientProvider>
    </LanguageProvider>
  );
}

function jsonResponse(body: unknown, status = 200) {
  return Promise.resolve(
    new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } }),
  );
}

beforeEach(() => {
  localStorage.clear();
  vi.restoreAllMocks();
  queryClient = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
});

describe("OpenAI settings", () => {
  it("loads an unconfigured state and clears the submitted secret without persisting or rendering it", async () => {
    const secret = "sk-proj-secret-value-ABCD";
    vi.spyOn(globalThis, "fetch").mockImplementation((_input, init) =>
      init?.method === "PUT" ? jsonResponse(active) : jsonResponse(unconfigured),
    );

    render(<OpenAISettingsPage />, { wrapper: Providers });

    expect(await screen.findByText("尚未配置凭据")).toBeInTheDocument();
    const input = screen.getByLabelText("OpenAI Project API Key");
    expect(input).toHaveAttribute("autocomplete", "new-password");
    fireEvent.change(input, { target: { value: secret } });
    fireEvent.click(screen.getByRole("button", { name: "保存并验证" }));

    await waitFor(() => expect(input).toHaveValue(""));
    expect(screen.getByText("ABCD1234")).toBeInTheDocument();
    expect(document.body).not.toHaveTextContent(secret);
    expect(localStorage.length).toBe(0);
    expect(queryClient.getMutationCache().getAll().map((mutation) => mutation.state.variables)).not.toContain(secret);
  });

  it("treats a missing setting as a usable empty state", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(() => jsonResponse({ code: "not_found" }, 404));
    render(<OpenAISettingsPage />, { wrapper: Providers });

    expect(await screen.findByText("尚未配置凭据")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "保存并验证" })).toBeEnabled();
  });

  it("renders an explicit permission state for forbidden access", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(() => jsonResponse({ code: "forbidden" }, 403));
    render(<OpenAISettingsPage />, { wrapper: Providers });

    expect(await screen.findByRole("alert")).toHaveTextContent("仅超级管理员可以管理 OpenAI 凭据");
    expect(screen.queryByLabelText("OpenAI Project API Key")).not.toBeInTheDocument();
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it("offers retry after a safe load error without displaying server details", async () => {
    const fetchMock = vi
      .spyOn(globalThis, "fetch")
      .mockImplementationOnce(() => jsonResponse({ error: "sk-proj-leaked-server-detail" }, 500))
      .mockImplementationOnce(() => jsonResponse(unconfigured));
    render(<OpenAISettingsPage />, { wrapper: Providers });

    expect(await screen.findByRole("alert")).toHaveTextContent("无法载入 OpenAI 设置");
    expect(document.body).not.toHaveTextContent("sk-proj-leaked-server-detail");
    expect(queryClient.getQueryCache().getAll().map((query) => errorMessage(query.state.error)).join(" ")).not.toContain("sk-proj-leaked-server-detail");
    fireEvent.click(screen.getByRole("button", { name: "重试载入 OpenAI 设置" }));
    expect(await screen.findByText("尚未配置凭据")).toBeInTheDocument();
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it("requires confirmation before replacing or disabling an active credential", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation((_input, init) => {
      if (init?.method === "PUT") return jsonResponse(active);
      if (init?.method === "DELETE") return jsonResponse({ ...active, status: "disabled" });
      return jsonResponse(active);
    });
    const confirm = vi.spyOn(window, "confirm").mockReturnValue(false);
    render(<OpenAISettingsPage />, { wrapper: Providers });
    const input = await screen.findByLabelText("OpenAI Project API Key");

    fireEvent.change(input, { target: { value: "sk-proj-replacement-value-ABCD" } });
    fireEvent.click(screen.getByRole("button", { name: "替换并验证" }));
    expect(confirm).toHaveBeenCalledWith(expect.stringContaining("替换"));
    expect(fetchMock.mock.calls.filter(([, init]) => init?.method === "PUT")).toHaveLength(0);

    fireEvent.click(screen.getByRole("button", { name: "停用 OpenAI" }));
    expect(confirm).toHaveBeenCalledWith(expect.stringContaining("停用"));
    expect(fetchMock.mock.calls.filter(([, init]) => init?.method === "DELETE")).toHaveLength(0);
  });

  it("shows validation, mutation failure, success status, and clears a shown secret", async () => {
    let putCount = 0;
    vi.spyOn(globalThis, "fetch").mockImplementation((_input, init) => {
      if (init?.method === "PUT") {
        putCount += 1;
        return putCount === 1
          ? jsonResponse({ error: "sk-proj-unsafe-rejection" }, 422)
          : jsonResponse(active);
      }
      return jsonResponse(unconfigured);
    });
    render(<OpenAISettingsPage />, { wrapper: Providers });
    const input = await screen.findByLabelText("OpenAI Project API Key");

    fireEvent.change(input, { target: { value: "short" } });
    fireEvent.click(screen.getByRole("button", { name: "保存并验证" }));
    expect(await screen.findByText("API Key 至少需要 20 个字符")).toBeInTheDocument();

    fireEvent.change(input, { target: { value: "sk-proj-unsafe-rejection-ABCD" } });
    fireEvent.click(screen.getByRole("button", { name: "显示 API Key" }));
    expect(input).toHaveAttribute("type", "text");
    fireEvent.click(screen.getByRole("button", { name: "保存并验证" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("OpenAI 未接受该凭据");
    expect(document.body).not.toHaveTextContent("sk-proj-unsafe-rejection-ABCD");
    expect(queryClient.getMutationCache().getAll().map((mutation) => errorMessage(mutation.state.error)).join(" ")).not.toContain("sk-proj-unsafe-rejection");

    fireEvent.click(screen.getByRole("button", { name: "保存并验证" }));
    expect(await screen.findByText("凭据已保存并验证")).toBeInTheDocument();
    expect(input).toHaveValue("");
    expect(input).toHaveAttribute("type", "password");
  });

  it("switches every fixed settings label to English immediately", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(() => jsonResponse(unconfigured));
    render(<OpenAISettingsPage />, { wrapper: Providers });
    await screen.findByText("尚未配置凭据");

    fireEvent.click(screen.getByRole("button", { name: "语言" }));
    expect(screen.getByRole("heading", { name: "OpenAI settings" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Save and verify" })).toBeInTheDocument();
    expect(screen.queryByText("尚未配置凭据")).not.toBeInTheDocument();
  });

  it("retranslates an active validation error when the language changes", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(() => jsonResponse(unconfigured));
    render(<OpenAISettingsPage />, { wrapper: Providers });
    const input = await screen.findByLabelText("OpenAI Project API Key");

    fireEvent.change(input, { target: { value: "short" } });
    fireEvent.click(screen.getByRole("button", { name: "保存并验证" }));
    expect(await screen.findByText("API Key 至少需要 20 个字符")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "语言" }));
    expect(screen.getByText("API Key must contain at least 20 characters")).toBeInTheDocument();
    expect(screen.queryByText("API Key 至少需要 20 个字符")).not.toBeInTheDocument();
  });

  it("exposes an accessible deep link for the super admin shell", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation((input) => String(input).includes("/auth/me") ? jsonResponse({ public_id: "00000000-0000-4000-8000-000000000001", name: "Owner", email: "owner@example.test", role: "super_admin", status: "active", must_change_password: false, last_seen_at: "2026-07-17T10:00:00Z", created_at: "2026-07-17T10:00:00Z" }) : jsonResponse(unconfigured));
    render(<AppShell><p>content</p></AppShell>, { wrapper: Providers });
    const links = await screen.findAllByRole("link", { name: "OpenAI 设置" });
    expect(links[0]).toHaveAttribute("href", "/settings/openai");
  });

  it("keeps restricted destinations visible and explained for operators", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(() => jsonResponse({ public_id: "00000000-0000-4000-8000-000000000003", name: "Operator", email: "operator@example.test", role: "operator", status: "active", must_change_password: false, last_seen_at: "2026-07-17T10:00:00Z", created_at: "2026-07-17T10:00:00Z" }));
    render(<AppShell><p>content</p></AppShell>, { wrapper: Providers });
    expect(await screen.findAllByText("仅超级管理员")).not.toHaveLength(0);
    expect(screen.getAllByText("需要管理员").length).toBeGreaterThanOrEqual(2);
    expect(screen.queryByRole("link", { name: "OpenAI 设置" })).not.toBeInTheDocument();
    expect(screen.getByLabelText("OpenAI 设置 · 仅超级管理员")).toBeDisabled();
  });
});

function errorMessage(error: unknown) {
  return error instanceof Error ? error.message : String(error ?? "");
}
