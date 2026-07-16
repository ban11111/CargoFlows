import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { beforeEach, expect, it, vi } from "vitest";

import { LanguageProvider } from "@/lib/i18n";

import SopTemplatesPage from "./page";

const sopID = "44444444-4444-4444-8444-444444444444";
const published = {
  schema_version: "1.0", public_id: "33333333-3333-4333-8333-333333333333", sop_public_id: sopID,
  version_number: 1, status: "published", name: { "zh-CN": "手机壳拍摄", en: "Phone Case Capture" },
  description: { "zh-CN": "", en: "" }, coordinate_system: { id: "pcs_object_v1", handedness: "right_handed", origin: "bounding_box_center", unit: "normalized", axes: { x_positive: "object_top", y_positive: "object_left", z_positive: "object_front" } }, views: [],
};
const draft = { ...published, public_id: "55555555-5555-4555-8555-555555555555", version_number: 2, status: "draft" };

function Providers({ children }: { children: ReactNode }) {
  return <LanguageProvider><QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: 30_000 } } })}>{children}</QueryClientProvider></LanguageProvider>;
}

beforeEach(() => { localStorage.clear(); vi.restoreAllMocks(); });

it("refreshes logical SOP details so an existing draft is visible", async () => {
  const requests: string[] = [];
  vi.spyOn(globalThis, "fetch").mockImplementation((input) => {
    const url = String(input);
    requests.push(url);
    const body = url.endsWith(`/capture-sops/${sopID}`)
      ? { public_id: sopID, category_id: 1, versions: [published, draft] }
      : url.endsWith("/capture-sops")
        ? { data: [{ public_id: sopID, category_id: 1, versions: [published] }] }
        : { data: [{ id: 1, name: "配件", name_en: "Accessories" }] };
    return Promise.resolve(new Response(JSON.stringify(body), { status: 200, headers: { "content-type": "application/json" } }));
  });

  render(<SopTemplatesPage />, { wrapper: Providers });

  expect(await screen.findByText("有草稿 · V2")).toBeInTheDocument();
  expect(requests).toContain(`/api/proxy/capture-sops/${sopID}`);
});
