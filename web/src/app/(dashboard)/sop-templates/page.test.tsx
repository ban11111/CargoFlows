import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { LanguageProvider } from "@/lib/i18n";

import SopTemplatesPage from "./page";

const sopID = "44444444-4444-4444-8444-444444444444";
const archivedSOPID = "88888888-8888-4888-8888-888888888888";
const published = {
  schema_version: "1.0", public_id: "33333333-3333-4333-8333-333333333333", sop_public_id: sopID,
  version_number: 1, status: "published", name: { "zh-CN": "手机壳拍摄", en: "Phone Case Capture" },
  updated_at: "2026-07-16T10:00:00.000Z",
  description: { "zh-CN": "", en: "" }, coordinate_system: { id: "pcs_object_v1", handedness: "right_handed", origin: "bounding_box_center", unit: "normalized", axes: { x_positive: "object_top", y_positive: "object_left", z_positive: "object_front" } }, views: [],
};
const draft = { ...published, public_id: "55555555-5555-4555-8555-555555555555", version_number: 2, status: "draft", name: { "zh-CN": "新建草稿 SOP", en: "New Draft SOP" } };
const archived = { ...published, public_id: "99999999-9999-4999-8999-999999999999", sop_public_id: archivedSOPID, status: "archived", name: { "zh-CN": "已归档 SOP", en: "Archived SOP" } };
const categoryResponse = { data: [{ id: 1, name: "配件", name_en: "Accessories" }] };

function Providers({ children }: { children: ReactNode }) {
  return <LanguageProvider><QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: 30_000 } } })}>{children}</QueryClientProvider></LanguageProvider>;
}

function jsonResponse(body: unknown, status = 200) {
  return Promise.resolve(new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } }));
}

beforeEach(() => { localStorage.clear(); vi.restoreAllMocks(); vi.spyOn(window, "confirm").mockReturnValue(true); });

describe("SOP management list", () => {
  it("requests admin lifecycle mode and renders draft-only and archived-only SOPs", async () => {
    const requests: string[] = [];
    const summaries = [
      { public_id: sopID, category_id: 1, versions: [draft] },
      { public_id: archivedSOPID, category_id: 1, versions: [archived] },
    ];
    vi.spyOn(globalThis, "fetch").mockImplementation((input) => {
      const url = String(input);
      requests.push(url);
      if (url.includes("/capture-sops?include_all=true")) return jsonResponse({ data: summaries });
      if (url.endsWith(`/capture-sops/${sopID}`)) return jsonResponse(summaries[0]);
      if (url.endsWith(`/capture-sops/${archivedSOPID}`)) return jsonResponse(summaries[1]);
      return jsonResponse(categoryResponse);
    });

    render(<SopTemplatesPage />, { wrapper: Providers });

    expect((await screen.findAllByText("新建草稿 SOP")).length).toBeGreaterThan(0);
    expect(screen.getAllByText("已归档 SOP").length).toBeGreaterThan(0);
    expect(requests).toContain("/api/proxy/capture-sops?include_all=true");
  });

  it("shows a list load error with an explicit retry", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation((input) => String(input).includes("/capture-sops") ? jsonResponse({ code: "failed" }, 500) : jsonResponse(categoryResponse));
    render(<SopTemplatesPage />, { wrapper: Providers });

    expect(await screen.findByText("无法载入 SOP 列表，请重试。")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "重试载入 SOP 列表" }));
    await waitFor(() => expect(fetchMock.mock.calls.filter(([url]) => String(url).includes("/capture-sops"))).toHaveLength(2));
  });

  it("shows a category load error without hiding SOP rows", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation((input) => {
      const url = String(input);
      if (url.includes("/categories")) return jsonResponse({ code: "failed" }, 500);
      if (url.includes("?include_all=true")) return jsonResponse({ data: [{ public_id: sopID, category_id: 1, versions: [published] }] });
      return jsonResponse({ public_id: sopID, category_id: 1, versions: [published] });
    });
    render(<SopTemplatesPage />, { wrapper: Providers });

    expect((await screen.findAllByText("手机壳拍摄")).length).toBeGreaterThan(0);
    expect(screen.getByText("无法载入分类；仍可按 SOP 名称浏览。")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "重试载入分类" })).toBeInTheDocument();
  });

  it("shows a per-SOP detail error and retry instead of silently using stale summary data", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation((input) => {
      const url = String(input);
      if (url.endsWith(`/capture-sops/${sopID}`)) return jsonResponse({ code: "failed" }, 500);
      if (url.includes("?include_all=true")) return jsonResponse({ data: [{ public_id: sopID, category_id: 1, versions: [published] }] });
      return jsonResponse(categoryResponse);
    });
    render(<SopTemplatesPage />, { wrapper: Providers });

    expect(await screen.findByText("该 SOP 的版本详情载入失败。")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "重试载入手机壳拍摄的版本详情" })).toBeInTheDocument();
  });

  it("lets an older published version be disabled and re-enabled", async () => {
    const newest = { ...published, public_id: "66666666-6666-4666-8666-666666666666", version_number: 2 };
    let summary = { public_id: sopID, category_id: 1, versions: [published, newest] };
    const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation((input, init) => {
      const url = String(input);
      if (url.endsWith(`/sop-versions/${published.public_id}/archive`) && init?.method === "POST") {
        const disabled = { ...published, status: "archived" };
        summary = { ...summary, versions: [disabled, newest] };
        return jsonResponse(disabled);
      }
      if (url.endsWith(`/sop-versions/${published.public_id}/restore`) && init?.method === "POST") {
        summary = { ...summary, versions: [published, newest] };
        return jsonResponse(published);
      }
      if (url.includes("/capture-sops?include_all=true")) return jsonResponse({ data: [summary] });
      if (url.endsWith(`/capture-sops/${sopID}`)) return jsonResponse(summary);
      return jsonResponse(categoryResponse);
    });

    render(<SopTemplatesPage />, { wrapper: Providers });

    expect(await screen.findByRole("link", { name: "打开版本 V1" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "打开版本 V2" })).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "停用版本 V1" }));

    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith(`/api/proxy/sop-versions/${published.public_id}/archive`, expect.objectContaining({ method: "POST" })));
    expect(await screen.findByText("已停用")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "停用版本 V1" })).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "重新启用 V1" }));

    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith(`/api/proxy/sop-versions/${published.public_id}/restore`, expect.objectContaining({ method: "POST" })));
    expect(await screen.findByRole("button", { name: "停用版本 V1" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "重新启用 V1" })).not.toBeInTheDocument();
  });
});
