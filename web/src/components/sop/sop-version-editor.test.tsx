import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import Link from "next/link";
import type { MouseEvent as ReactMouseEvent, ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { LanguageProvider } from "@/lib/i18n";
import type { SOPVersion } from "@/lib/sop";

import { SOPVersionEditor } from "./sop-version-editor";

const referenceID = "11111111-1111-4111-8111-111111111111";
const backID = "22222222-2222-4222-8222-222222222222";
const versionID = "33333333-3333-4333-8333-333333333333";
const sopID = "44444444-4444-4444-8444-444444444444";
const detailID = "77777777-7777-4777-8777-777777777777";
const initialRevision = "2026-07-16T10:00:00.000Z";

const referenceView = {
  public_id: referenceID,
  sequence: 1,
  role: "reference_front" as const,
  view_kind: "standard" as const,
  preset_key: "reference_front",
  name: { "zh-CN": "正面", en: "Front" },
  instruction: { "zh-CN": "商品正面对准相机。", en: "Face the product toward the camera." },
  required: true,
  allow_multiple: false,
  pose: {
    space: "object" as const,
    camera_position_direction: [0, 0, 1] as [number, number, number],
    image_up_direction: [1, 0, 0] as [number, number, number],
    target: [0, 0, 0] as [number, number, number],
  },
  composition: {
    frame_occupancy: 0.85,
    aspect_ratio: "1:1",
    allow_rotation_correction: true,
    allow_mirror: false as const,
  },
  reference_images: [],
};

const backView = {
  ...referenceView,
  public_id: backID,
  sequence: 2,
  role: "capture" as const,
  preset_key: "back",
  name: { "zh-CN": "背面", en: "Back" },
  instruction: { "zh-CN": "拍摄商品背面。", en: "Capture the product back." },
  pose: { ...referenceView.pose, camera_position_direction: [0, 0, -1] as [number, number, number] },
};

const detailView = {
  ...backView,
  public_id: detailID,
  sequence: 3,
  preset_key: "detail_label",
  view_kind: "detail" as const,
  name: { "zh-CN": "标签细节", en: "Label Detail" },
  required: false,
};

const draftFixture: SOPVersion = {
  schema_version: "1.0",
  public_id: versionID,
  sop_public_id: sopID,
  version_number: 1,
  status: "draft",
  updated_at: initialRevision,
  name: { "zh-CN": "手机壳拍摄", en: "Phone Case Capture" },
  description: { "zh-CN": "电商拍摄规范", en: "E-commerce capture specification" },
  coordinate_system: {
    id: "pcs_object_v1",
    handedness: "right_handed",
    origin: "bounding_box_center",
    unit: "normalized",
    axes: { x_positive: "object_top", y_positive: "object_left", z_positive: "object_front" },
  },
  views: [referenceView, backView],
};

function response(body: unknown, status = 200) {
  return Promise.resolve(new Response(status === 204 ? undefined : JSON.stringify(body), {
    status,
    headers: { "content-type": "application/json" },
  }));
}

function Providers({ children }: { children: ReactNode }) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return (
    <LanguageProvider>
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    </LanguageProvider>
  );
}

function renderEditor(version: SOPVersion = draftFixture) {
  return render(<SOPVersionEditor initialVersion={version} />, { wrapper: Providers });
}

function renderEditorWithClient(version: SOPVersion = draftFixture) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  const rendered = render(
    <LanguageProvider><QueryClientProvider client={client}><SOPVersionEditor initialVersion={version} /></QueryClientProvider></LanguageProvider>,
  );
  return { ...rendered, client };
}

function renderEditorWithBackLink(onNavigate: (event: ReactMouseEvent<HTMLAnchorElement>) => void) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(
    <LanguageProvider><QueryClientProvider client={client}>
      <Link href="/sop-templates" onClick={onNavigate}>返回 SOP 列表</Link>
      <SOPVersionEditor initialVersion={draftFixture} />
    </QueryClientProvider></LanguageProvider>,
  );
}

describe("SOPVersionEditor", () => {
  beforeEach(() => {
    localStorage.clear();
    vi.restoreAllMocks();
    window.history.replaceState({}, "", `/sop-templates/${sopID}/versions/${versionID}`);
  });

  it("renders the reference front as locked but leaves its text editable", () => {
    renderEditor();

    const reference = screen.getByTestId(`sop-view-${referenceID}`);
    expect(within(reference).getByDisplayValue("正面")).toBeEnabled();
    expect(within(reference).getByLabelText("正面 必拍")).toBeDisabled();
    expect(within(reference).getByLabelText("正面 相机位置方向 X")).toBeDisabled();
    expect(within(reference).getByRole("button", { name: "删除正面" })).toBeDisabled();
    expect(within(reference).getByText("顺序、类型、必拍状态和方向已锁定")).toBeInTheDocument();
  });

  it("adds packaging front as an optional preset through the real endpoint", async () => {
    const packaging = {
      ...backView,
      public_id: "55555555-5555-4555-8555-555555555555",
      sequence: 3,
      preset_key: "packaging_front",
      name: { "zh-CN": "包装正面", en: "Packaging Front" },
      required: false,
      pose: referenceView.pose,
    };
    const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(() => response({ ...draftFixture, views: [...draftFixture.views, packaging] }));
    renderEditor();

    fireEvent.click(screen.getByRole("button", { name: "添加包装正面" }));

    expect(await screen.findByDisplayValue("Packaging Front")).toBeInTheDocument();
    expect(screen.getByLabelText("Packaging Front 必拍")).not.toBeChecked();
    expect(fetchMock).toHaveBeenCalledWith(
      `/api/proxy/sop-versions/${versionID}/views`,
      expect.objectContaining({ method: "POST", body: JSON.stringify({ preset_key: "packaging_front" }), headers: expect.objectContaining({ "X-SOP-Version-Updated-At": initialRevision }) }),
    );
  });

  it("chains the confirmed updated_at token across draft mutations", async () => {
    const nextRevision = "2026-07-16T10:00:00.001Z";
    const fetchMock = vi.spyOn(globalThis, "fetch")
      .mockImplementationOnce(() => response({ ...draftFixture, updated_at: nextRevision, name: { ...draftFixture.name, "zh-CN": "第一次" } }))
      .mockImplementationOnce(() => response({ ...draftFixture, updated_at: "2026-07-16T10:00:00.002Z", name: { ...draftFixture.name, "zh-CN": "第二次" } }));
    renderEditor();

    fireEvent.change(screen.getByLabelText("SOP 中文名称"), { target: { value: "第一次" } });
    fireEvent.click(screen.getByRole("button", { name: "保存版本信息" }));
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));
    fireEvent.change(screen.getByLabelText("SOP 中文名称"), { target: { value: "第二次" } });
    fireEvent.click(screen.getByRole("button", { name: "保存版本信息" }));
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2));

    expect((fetchMock.mock.calls[0][1] as RequestInit).headers).toEqual(expect.objectContaining({ "X-SOP-Version-Updated-At": initialRevision }));
    expect((fetchMock.mock.calls[1][1] as RequestInit).headers).toEqual(expect.objectContaining({ "X-SOP-Version-Updated-At": nextRevision }));
  });

  it.each(["published", "archived"] as const)("makes every field read-only for a %s version", (status) => {
    renderEditor({ ...draftFixture, status });

    expect(screen.getByText(status === "published" ? "已发布版本不可修改" : "已归档版本不可修改")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "添加视图" })).toBeDisabled();
    expect(screen.getByLabelText("SOP 中文名称")).toHaveAttribute("readonly");
    expect(screen.getByDisplayValue("背面")).toHaveAttribute("readonly");
    expect(screen.getByRole("button", { name: "保存版本信息" })).toBeDisabled();
  });

  it("validates before publishing and only publishes when no issues remain", async () => {
    const calls: string[] = [];
    vi.spyOn(globalThis, "fetch").mockImplementation((input) => {
      const url = String(input);
      calls.push(url);
      if (url.endsWith("/validate")) return response({ code: "sop_valid", errors: [] });
      if (url.endsWith("/publish")) return response({ ...draftFixture, status: "published" });
      throw new Error(`Unexpected request ${url}`);
    });
    renderEditor();

    fireEvent.click(screen.getByRole("button", { name: "发布版本" }));

    expect(await screen.findByText("已发布版本不可修改")).toBeInTheDocument();
    expect(calls).toEqual([
      `/api/proxy/sop-versions/${versionID}/validate`,
      `/api/proxy/sop-versions/${versionID}/publish`,
    ]);
  });

  it("stops publication, summarizes server errors, and associates their field paths", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(() => response({
      code: "sop_validation_failed",
      errors: [{
        code: "pose_vectors_parallel",
        path: "views[1].pose.image_up_direction",
        message: { "zh-CN": "相机方向与图片向上方向不能平行。", en: "Directions cannot be parallel." },
      }],
    }));
    renderEditor();

    fireEvent.click(screen.getByRole("button", { name: "发布版本" }));

    const summary = await screen.findByRole("alert");
    expect(summary).toHaveTextContent("请修正以下问题后再发布");
    expect(summary).toHaveTextContent("相机方向与图片向上方向不能平行。");
    expect(within(summary).getByRole("link", { name: /相机方向与图片向上方向不能平行/ })).toHaveAttribute("href", `#view-${backID}`);
    expect(screen.getByLabelText("背面 画面向上方向 X")).toHaveAttribute("aria-invalid", "true");
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it("sends a complete UUID list when moving an interior view", async () => {
    const initial = { ...draftFixture, views: [referenceView, backView, detailView] };
    const reordered = { ...initial, views: [referenceView, { ...detailView, sequence: 2 }, { ...backView, sequence: 3 }] };
    const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(() => response(reordered));
    renderEditor(initial);

    fireEvent.click(screen.getByRole("button", { name: "标签细节上移" }));
    await waitFor(() => expect(fetchMock).toHaveBeenCalled());

    expect(fetchMock).toHaveBeenCalledWith(
      `/api/proxy/sop-versions/${versionID}/view-order`,
      expect.objectContaining({
        method: "PUT",
        body: JSON.stringify({ public_ids: [referenceID, detailID, backID] }),
      }),
    );
  });

  it("disables both reference controls and every boundary move without a request", () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(() => response(draftFixture));
    renderEditor();

    expect(screen.getByRole("button", { name: "正面上移" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "正面下移" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "背面上移" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "背面下移" })).toBeDisabled();
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("preserves an unsaved title when a view save returns an older aggregate", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(() => response(draftFixture));
    renderEditor();

    fireEvent.change(screen.getByLabelText("SOP 中文名称"), { target: { value: "未保存的新标题" } });
    fireEvent.change(screen.getByLabelText("背面 相机位置方向 X"), { target: { value: "0.125" } });
    fireEvent.click(screen.getByRole("button", { name: "保存背面" }));

    await waitFor(() => expect(screen.getByLabelText("SOP 中文名称")).toHaveValue("未保存的新标题"));
    expect(screen.getByRole("button", { name: "发布版本" })).toBeDisabled();
  });

  it("blocks publication and aggregate mutations while edits are dirty", () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(() => response(draftFixture));
    renderEditor();

    fireEvent.change(screen.getByLabelText("SOP 中文名称"), { target: { value: "未保存的新标题" } });

    expect(screen.getByText("请先保存所有未保存的修改，再执行发布、排序或新增视图。")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "发布版本" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "添加包装正面" })).toBeDisabled();
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("publishes the server-confirmed revision only after every dirty section is saved", async () => {
    const calls: string[] = [];
    const savedTitle = { ...draftFixture, name: { ...draftFixture.name, "zh-CN": "新标题" } };
    const savedBack = { ...backView, pose: { ...backView.pose, camera_position_direction: [0.25, 0, -1] as [number, number, number] } };
    vi.spyOn(globalThis, "fetch").mockImplementation((input) => {
      const url = String(input);
      calls.push(url);
      if (url.endsWith(`/views/${backID}`)) return response({ ...savedTitle, views: [referenceView, savedBack] });
      if (url.endsWith(`/sop-versions/${versionID}`)) return response(savedTitle);
      if (url.endsWith("/validate")) return response({ code: "sop_valid", errors: [] });
      if (url.endsWith("/publish")) return response({ ...savedTitle, status: "published", views: [referenceView, savedBack] });
      throw new Error(`Unexpected request ${url}`);
    });
    renderEditor();

    fireEvent.change(screen.getByLabelText("SOP 中文名称"), { target: { value: "新标题" } });
    fireEvent.change(screen.getByLabelText("背面 相机位置方向 X"), { target: { value: "0.25" } });
    fireEvent.click(screen.getByRole("button", { name: "保存版本信息" }));
    await waitFor(() => expect(calls).toHaveLength(1));
    expect(screen.getByLabelText("背面 相机位置方向 X")).toHaveValue(0.25);
    expect(screen.getByRole("button", { name: "发布版本" })).toBeDisabled();

    fireEvent.click(screen.getByRole("button", { name: "保存背面" }));
    await waitFor(() => expect(screen.getByRole("button", { name: "发布版本" })).toBeEnabled());
    fireEvent.click(screen.getByRole("button", { name: "发布版本" }));

    expect(await screen.findByText("已发布版本不可修改")).toBeInTheDocument();
    expect(calls).toEqual([
      `/api/proxy/sop-versions/${versionID}`,
      `/api/proxy/sop-versions/${versionID}/views/${backID}`,
      `/api/proxy/sop-versions/${versionID}/validate`,
      `/api/proxy/sop-versions/${versionID}/publish`,
    ]);
  });

  it("protects dirty navigation and clears the guard after a confirmed save", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(() => response({ ...draftFixture, name: { ...draftFixture.name, "zh-CN": "新标题" } }));
    renderEditor();
    fireEvent.change(screen.getByLabelText("SOP 中文名称"), { target: { value: "新标题" } });

    const dirtyEvent = new Event("beforeunload", { cancelable: true });
    window.dispatchEvent(dirtyEvent);
    expect(dirtyEvent.defaultPrevented).toBe(true);

    fireEvent.click(screen.getByRole("button", { name: "保存版本信息" }));
    await waitFor(() => expect(screen.getByRole("button", { name: "发布版本" })).toBeEnabled());
    const cleanEvent = new Event("beforeunload", { cancelable: true });
    window.dispatchEvent(cleanEvent);
    expect(cleanEvent.defaultPrevented).toBe(false);
  });

  it("blocks an actual same-origin back anchor when navigation is cancelled", () => {
    const onNavigate = vi.fn();
    vi.spyOn(window, "confirm").mockReturnValue(false);
    renderEditorWithBackLink(onNavigate);
    fireEvent.change(screen.getByLabelText("SOP 中文名称"), { target: { value: "未保存" } });

    fireEvent.click(screen.getByRole("link", { name: "返回 SOP 列表" }));

    expect(window.confirm).toHaveBeenCalledWith("有未保存的修改。离开后这些修改将丢失，确定离开吗？");
    expect(onNavigate).not.toHaveBeenCalled();
    expect(window.location.pathname).toContain(`/versions/${versionID}`);
  });

  it("continues an internal anchor navigation after confirmation without a prompt loop", async () => {
    const onNavigate = vi.fn((event: ReactMouseEvent<HTMLAnchorElement>) => event.preventDefault());
    vi.spyOn(window, "confirm").mockReturnValue(true);
    renderEditorWithBackLink(onNavigate);
    fireEvent.change(screen.getByLabelText("SOP 中文名称"), { target: { value: "未保存" } });

    fireEvent.click(screen.getByRole("link", { name: "返回 SOP 列表" }));

    await waitFor(() => expect(onNavigate).toHaveBeenCalledTimes(1));
    expect(window.confirm).toHaveBeenCalledTimes(1);
  });

  it("does not guard clean, external, download, new-tab, or modifier navigation", () => {
    const confirm = vi.spyOn(window, "confirm").mockReturnValue(false);
    const { container } = renderEditorWithBackLink(() => undefined);
    fireEvent.click(screen.getByRole("link", { name: "返回 SOP 列表" }));
    expect(confirm).not.toHaveBeenCalled();

    fireEvent.change(screen.getByLabelText("SOP 中文名称"), { target: { value: "未保存" } });
    const external = document.createElement("a"); external.href = "https://example.com"; external.target = "_blank"; external.textContent = "external"; container.append(external);
    const download = document.createElement("a"); download.href = "/export"; download.download = "sop.json"; download.textContent = "download"; container.append(download);
    const hash = document.createElement("a"); hash.href = `${window.location.pathname}#validation`; hash.textContent = "hash"; container.append(hash);
    fireEvent.click(external);
    fireEvent.click(download);
    fireEvent.click(hash);
    fireEvent.click(screen.getByRole("link", { name: "返回 SOP 列表" }), { metaKey: true });
    expect(confirm).not.toHaveBeenCalled();
  });

  it("preserves exact numeric vector input in a view update", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(() => response(draftFixture));
    renderEditor();

    fireEvent.change(screen.getByLabelText("背面 相机位置方向 X"), { target: { value: "0.125" } });
    fireEvent.click(screen.getByRole("button", { name: "保存背面" }));
    await waitFor(() => expect(fetchMock).toHaveBeenCalled());

    const request = fetchMock.mock.calls[0][1] as RequestInit;
    const body = JSON.parse(String(request.body));
    expect(body.pose.camera_position_direction).toEqual([0.125, 0, -1]);
    expect(body).not.toHaveProperty("public_id");
    expect(body).not.toHaveProperty("sequence");
  });

  it("uploads a reference image before registering its metadata", async () => {
    const completionToken = "55555555-5555-4555-8555-555555555555";
    const calls: Array<{ url: string; method?: string }> = [];
    vi.spyOn(globalThis, "fetch").mockImplementation((input, init) => {
      const url = String(input);
      calls.push({ url, method: init?.method });
      if (url.endsWith("/upload-url")) return response({
        method: "PUT", upload_url: "https://uploads.example/sample.jpg", completion_token: completionToken,
        expires_in: 900, headers: { "content-type": "image/jpeg" },
      });
      if (url === "https://uploads.example/sample.jpg") return response(undefined, 204);
      if (url.endsWith("/reference-images")) return response({ ...draftFixture, updated_at: "2026-07-16T10:00:00.001Z", views: [referenceView, { ...backView, reference_images: [{
        public_id: "66666666-6666-4666-8666-666666666666",
        thumbnail_url: "/api/v1/sop-reference-images/66666666-6666-4666-8666-666666666666/media", sort_order: 1, caption: { "zh-CN": "", en: "" },
      }] }] }, 201);
      throw new Error(`Unexpected request ${url}`);
    });
    const { client } = renderEditorWithClient();
    client.setQueryData(["sop-version", versionID], draftFixture);
    const file = new File(["pixels"], "sample.jpg", { type: "image/jpeg" });

    fireEvent.change(screen.getByLabelText("背面参考图中文说明"), { target: { value: "包装细节示例" } });
    fireEvent.change(screen.getByLabelText("Back reference caption"), { target: { value: "Packaging detail example" } });
    fireEvent.change(screen.getByLabelText("为背面添加参考图"), { target: { files: [file] } });

    await waitFor(() => expect(calls).toHaveLength(3));
    expect(calls).toEqual([
      { url: `/api/proxy/sop-versions/${versionID}/views/${backID}/reference-images/upload-url`, method: "POST" },
      { url: "https://uploads.example/sample.jpg", method: "PUT" },
      { url: `/api/proxy/sop-versions/${versionID}/views/${backID}/reference-images`, method: "POST" },
    ]);
    const metadataRequest = (globalThis.fetch as ReturnType<typeof vi.fn>).mock.calls[2][1] as RequestInit;
    expect(JSON.parse(String(metadataRequest.body))).toEqual({
      completion_token: completionToken,
      caption: { "zh-CN": "包装细节示例", en: "Packaging detail example" },
    });
    const cached = client.getQueryData<SOPVersion>(["sop-version", versionID]);
    expect(cached?.views[1].reference_images).toHaveLength(1);
    expect(client.getQueryState(["sop-version", versionID])?.isInvalidated).toBe(true);
  });

  it("updates and invalidates the exact version cache after reference reorder and delete", async () => {
    const firstImage = { public_id: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", thumbnail_url: "/one.jpg", sort_order: 1, caption: { "zh-CN": "一", en: "One" } };
    const secondImage = { public_id: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", thumbnail_url: "/two.jpg", sort_order: 2, caption: { "zh-CN": "二", en: "Two" } };
    const withImages = { ...draftFixture, views: [referenceView, { ...backView, reference_images: [firstImage, secondImage] }] };
    const reordered = { ...withImages, views: [referenceView, { ...backView, reference_images: [{ ...secondImage, sort_order: 1 }, { ...firstImage, sort_order: 2 }] }] };
    vi.spyOn(window, "confirm").mockReturnValue(true);
    const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation((input, init) => {
      if (init?.method === "PUT") return response(reordered);
      if (init?.method === "DELETE") return response({ ...reordered, updated_at: "2026-07-16T10:00:00.002Z", views: [referenceView, { ...backView, reference_images: [{ ...firstImage, sort_order: 1 }] }] });
      throw new Error(`Unexpected request ${String(input)}`);
    });
    const { client } = renderEditorWithClient(withImages);
    client.setQueryData(["sop-version", versionID], withImages);

    fireEvent.click(screen.getByRole("button", { name: "参考图上移 2" }));
    await waitFor(() => expect(client.getQueryData<SOPVersion>(["sop-version", versionID])?.views[1].reference_images[0].public_id).toBe(secondImage.public_id));
    expect(client.getQueryState(["sop-version", versionID])?.isInvalidated).toBe(true);

    fireEvent.click(screen.getByRole("button", { name: "删除参考图 1" }));
    await waitFor(() => expect(client.getQueryData<SOPVersion>(["sop-version", versionID])?.views[1].reference_images).toHaveLength(1));
    expect(client.getQueryData<SOPVersion>(["sop-version", versionID])?.views[1].reference_images[0].public_id).toBe(firstImage.public_id);
    expect(client.getQueryState(["sop-version", versionID])?.isInvalidated).toBe(true);
    expect(fetchMock).toHaveBeenCalledWith(`/api/proxy/sop-versions/${versionID}/views/${backID}/reference-images/${secondImage.public_id}`, expect.objectContaining({ method: "DELETE" }));
  });
});
