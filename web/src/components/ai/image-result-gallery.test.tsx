import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";

import { ImageResultGallery } from "./image-result-gallery";

function Providers({ children }: { children: ReactNode }) {
  return <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>{children}</QueryClientProvider>;
}

describe("ImageResultGallery", () => {
  it("refetches when the parent job snapshot changes to completed", async () => {
    let resultReady = false;
    const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(async () => new Response(JSON.stringify({ data: resultReady ? [{
      public_id: "result-1",
      job_item_id: "item-1",
      candidate_index: 1,
      media_url: "/api/v1/ai-image-results/result-1/media",
      mime_type: "image/png",
      width: 1024,
      height: 1024,
      byte_count: 2048,
      sha256: "a".repeat(64),
      provider: "openai",
      model: "gpt-image-1.5",
      created_at: "2026-07-20T00:00:02Z",
    }] : [] }), { status: 200 }));

    const view = render(<ImageResultGallery active jobID="job-1" language="zh" refreshKey="2026-07-20T00:00:01Z" />, { wrapper: Providers });
    await waitFor(() => expect(fetchMock).toHaveBeenCalled());
    expect(screen.queryByRole("img")).not.toBeInTheDocument();

    resultReady = true;
    view.rerender(<ImageResultGallery active={false} itemLabels={{ "item-1": "画布 1：白底主图 + 卖点图" }} jobID="job-1" language="zh" refreshKey="2026-07-20T00:00:02Z" />);

    expect(await screen.findByRole("img", { name: "生成图片候选 1" })).toBeInTheDocument();
    expect(screen.getByText("画布 1：白底主图 + 卖点图")).toBeInTheDocument();
    expect(fetchMock.mock.calls.length).toBeGreaterThanOrEqual(2);
  });
});
