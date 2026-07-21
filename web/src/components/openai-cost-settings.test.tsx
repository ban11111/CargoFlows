import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { OpenAICostSettings } from "@/components/openai-cost-settings";

let queryClient: QueryClient;

function Providers({ children }: { children: ReactNode }) {
  return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
}

function jsonResponse(body: unknown, status = 200) {
  return Promise.resolve(new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } }));
}

beforeEach(() => {
  vi.restoreAllMocks();
  queryClient = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
});

describe("OpenAI cost settings", () => {
  it("configures project-wide cost synchronization without an API key binding", async () => {
    const bodies: Array<Record<string, unknown>> = [];
    vi.spyOn(globalThis, "fetch").mockImplementation((input, init) => {
      if (init?.body) bodies.push(JSON.parse(String(init.body)) as Record<string, unknown>);
      if (String(input).includes("/settings/openai/costs/scopes")) {
        return jsonResponse({ projects: [{ id: "proj_cargoflows", name: "CargoFlows" }], api_keys: [] });
      }
      return jsonResponse({ status: "active", admin_key_fingerprint: "ABCD", project_id: "proj_cargoflows", scope: "project", last_synced_at: null });
    });

    render(<OpenAICostSettings />, { wrapper: Providers });

    expect(screen.queryByLabelText("CargoFlows 独占 API Key")).not.toBeInTheDocument();
    expect(screen.getByText(/更换 Project API Key 后历史费用仍会保留/)).toBeInTheDocument();
    const adminKey = screen.getByLabelText("Admin API Key");
    fireEvent.change(adminKey, { target: { value: "sk-admin-project-scope-secret" } });
    fireEvent.click(screen.getByRole("button", { name: "读取 Project 列表" }));

    const project = await screen.findByLabelText("CargoFlows 独占 Project");
    await waitFor(() => expect(screen.getByRole("option", { name: "CargoFlows · proj_cargoflows" })).toBeInTheDocument());
    fireEvent.change(project, { target: { value: "proj_cargoflows" } });
    const save = screen.getByRole("button", { name: "验证并保存" });
    expect(save).toBeEnabled();
    fireEvent.click(save);

    await waitFor(() => expect(adminKey).toHaveValue(""));
    expect(bodies).toEqual([
      { admin_api_key: "sk-admin-project-scope-secret" },
      { admin_api_key: "sk-admin-project-scope-secret", project_id: "proj_cargoflows" },
    ]);
    expect(bodies.some((body) => "api_key_id" in body)).toBe(false);
  });

  it("shows an existing binding as project-wide even when legacy metadata is returned", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(() => jsonResponse({
      status: "active",
      admin_key_fingerprint: "ABCD",
      project_id: "proj_cargoflows",
      api_key_id: "key_legacy",
      scope: "project",
      last_synced_at: null,
    }));

    render(<OpenAICostSettings />, { wrapper: Providers });
    fireEvent.click(screen.getByRole("button", { name: "读取当前绑定" }));

    expect(await screen.findByText(/整个 Project（支持 Key 轮换）/)).toBeInTheDocument();
    expect(screen.queryByText("key_legacy")).not.toBeInTheDocument();
  });
});
