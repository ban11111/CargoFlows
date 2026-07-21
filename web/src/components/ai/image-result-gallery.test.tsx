import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { ImageResultGallery } from "./image-result-gallery";

function Providers({ children }: { children: ReactNode }) {
  return <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>{children}</QueryClientProvider>;
}

const thread = {
  public_id: "thread-1", job_item_id: "item-1", slot_key: "hero", selected_result_id: "result-1",
  turns: [{ public_id: "turn-1", sequence: 1, operation: "generate", user_instruction: "", mask_present: false, status: "completed", actor: { public_id: "user-1", name: "Ada", email: "ada@example.test" }, safe_error: "", requested_model: "gpt-image-2", actual_model: "gpt-image-2", api_mode: "images", provider_request_id: "req_123", failure_code: "", created_at: "2026-07-20T00:00:02Z", results: [{ public_id: "result-1", candidate_index: 1, media_url: "/api/v1/ai-jobs/job-1/image-results/result-1/media", selected: true, created_at: "2026-07-20T00:00:02Z" }, { public_id: "result-2", candidate_index: 1, media_url: "/api/v1/ai-jobs/job-1/image-results/result-2/media", selected: false, created_at: "2026-07-20T00:00:03Z" }] }],
};

afterEach(() => vi.restoreAllMocks());

describe("ImageResultGallery", () => {
  it("shows permanent turns, actor, actual model, and the adopted version", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(JSON.stringify({ data: [thread] }), { status: 200 }));
    render(<ImageResultGallery active={false} itemLabels={{ "item-1": "白底主图" }} jobID="job-1" language="zh" refreshKey="done" />, { wrapper: Providers });
    expect(await screen.findByRole("heading", { name: "白底主图" })).toBeInTheDocument();
    expect(screen.getByText(/Ada/)).toBeInTheDocument();
    expect(screen.getByText(/gpt-image-2/)).toBeInTheDocument();
    expect(screen.getByText("采用版本")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "提交到素材库审核" })).toBeInTheDocument();
  });

  it("selects any historical result through the slot-scoped endpoint", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(async (_input, init) => {
      if (init?.method === "POST") return new Response(JSON.stringify({ selected_result_id: "result-2" }), { status: 200 });
      return new Response(JSON.stringify({ data: [thread] }), { status: 200 });
    });
    render(<ImageResultGallery active={false} jobID="job-1" language="zh" refreshKey="done" />, { wrapper: Providers });
    const adopt = (await screen.findAllByRole("button", { name: "采用" })).find((button) => !button.hasAttribute("disabled"));
    expect(adopt).toBeDefined(); fireEvent.click(adopt!);
    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith("/api/proxy/ai-jobs/job-1/items/item-1/image-results/result-2/select", expect.objectContaining({ method: "POST" })));
  });

  it("shows a before-and-after comparison for an edited historical result", async () => {
    const editedThread = {
      ...thread,
      turns: [...thread.turns, { ...thread.turns[0], public_id: "turn-2", sequence: 2, operation: "edit", parent_result_id: "result-1", results: [{ public_id: "result-3", candidate_index: 1, parent_result_id: "result-1", media_url: "/api/v1/ai-jobs/job-1/image-results/result-3/media", selected: false, created_at: "2026-07-20T00:01:00Z" }] }],
    };
    vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(JSON.stringify({ data: [editedThread] }), { status: 200 }));
    render(<ImageResultGallery active={false} jobID="job-1" language="zh" refreshKey="done" />, { wrapper: Providers });
    expect(await screen.findByText("修改前")).toBeInTheDocument();
    expect(screen.getByText("修改后")).toBeInTheDocument();
  });
});
