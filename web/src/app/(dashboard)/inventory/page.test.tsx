import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import InventoryPage from "./page";

const sku = {
  public_id: "11111111-1111-4111-8111-111111111111",
  code: "CASE-BLK",
  stock: 10,
  average_unit_cost_sgd: "8.00000000",
  inventory_value_sgd: "80.00000000",
  costing_warning: false,
};

const transaction = {
  public_id: "22222222-2222-4222-8222-222222222222",
  type: "purchase_receipt",
  status: "posted",
  business_date: "2026-07-20T00:00:00Z",
  note: "PO-1",
  created_at: "2026-07-20T00:00:00Z",
  updated_at: "2026-07-20T00:00:00Z",
  charges: [],
  lines: [{
    public_id: "33333333-3333-4333-8333-333333333333",
    sku,
    quantity_delta: 10,
    source_currency: "SGD",
    source_unit_price: "8.00000000",
    fx_rate_to_sgd: "1.00000000",
    fx_rate_source: "manual",
    merchandise_amount_sgd: "80.00000000",
    allocated_charges_sgd: "0.00000000",
    landed_unit_cost_sgd: "8.00000000",
    movement_cost_sgd: "80.00000000",
    quantity_before: 0,
    quantity_after: 10,
    average_cost_before_sgd: "0.00000000",
    average_cost_after_sgd: "8.00000000",
    inventory_value_before_sgd: "0.00000000",
    inventory_value_after_sgd: "80.00000000",
    effective_merchandise_amount_sgd: "80.00000000",
    effective_allocated_charges_sgd: "0.00000000",
    effective_landed_unit_cost_sgd: "8.00000000",
    effective_movement_cost_sgd: "80.00000000",
    effective_average_cost_before_sgd: "0.00000000",
    effective_average_cost_after_sgd: "8.00000000",
    effective_inventory_value_before_sgd: "0.00000000",
    effective_inventory_value_after_sgd: "80.00000000",
    cost_version: 0,
  }],
};

const preview = {
  strategy: "moving_average_recost",
  inventory_value_delta_sgd: "20.00000000",
  historical_outflow_cost_delta_sgd: "0.00000000",
  impacts: [{ sku_id: sku.public_id, sku_code: sku.code, current_average_cost_sgd: "8.00000000", effective_average_cost_sgd: "10.00000000", current_inventory_value_sgd: "80.00000000", effective_inventory_value_sgd: "100.00000000", inventory_value_delta_sgd: "20.00000000", historical_outflow_cost_delta_sgd: "0.00000000", affected_transaction_count: 1 }],
};

function Providers({ children }: { children: ReactNode }) {
  return <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>{children}</QueryClientProvider>;
}

function jsonResponse(body: unknown, status = 200) {
  return Promise.resolve(new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } }));
}

beforeEach(() => {
  vi.restoreAllMocks();
});

describe("inventory correction workflow", () => {
  it("previews and submits a cost correction without editing the posted transaction", async () => {
    const requests: Array<{ url: string; method?: string; body?: unknown }> = [];
    vi.spyOn(globalThis, "fetch").mockImplementation((input, init) => {
      const url = String(input);
      requests.push({ url, method: init?.method, body: init?.body ? JSON.parse(String(init.body)) : undefined });
      if (url.endsWith("/skus")) return jsonResponse({ data: [sku] });
      if (url.includes("/corrections/preview")) return jsonResponse(preview);
      if (url.endsWith("/corrections") && init?.method === "POST") return jsonResponse({ ...preview, correction: { public_id: "44444444-4444-4444-8444-444444444444", kind: "cost", reason: "invoice typo", version: 1, inventory_value_delta_sgd: "20.00000000", historical_outflow_cost_delta_sgd: "0.00000000", created_at: "2026-07-21T00:00:00Z" } }, 201);
      return jsonResponse({ data: [transaction] });
    });

    render(<InventoryPage />, { wrapper: Providers });
    fireEvent.click(await screen.findByRole("button", { name: "纠正交易" }));
    fireEvent.change(screen.getByLabelText("纠正原因（必填）"), { target: { value: "invoice typo" } });
    fireEvent.change(screen.getByLabelText("正确原币单价"), { target: { value: "10" } });
    fireEvent.click(screen.getByRole("button", { name: "预览影响" }));

    expect(await screen.findByText("影响预览")).toBeInTheDocument();
    expect(screen.getByText("S$8.00 → S$10.00")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "确认纠正" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());

    const previewRequest = requests.find((item) => item.url.includes("/corrections/preview"));
    const submitRequest = requests.find((item) => item.url.endsWith("/corrections") && item.method === "POST");
    expect(previewRequest?.body).toMatchObject({ kind: "cost", reason: "invoice typo", lines: [{ sku_id: sku.public_id, quantity: 10, source_unit_price: "10" }] });
    expect(submitRequest?.body).toEqual(previewRequest?.body);
  });

  it("edits a draft in place instead of creating a replacement transaction", async () => {
    const draft = { ...transaction, status: "draft" };
    const requests: Array<{ url: string; method?: string }> = [];
    vi.spyOn(globalThis, "fetch").mockImplementation((input, init) => {
      const url = String(input);
      requests.push({ url, method: init?.method });
      if (url.endsWith("/skus")) return jsonResponse({ data: [sku] });
      if (init?.method === "PATCH") return jsonResponse(draft);
      return jsonResponse({ data: [draft] });
    });

    render(<InventoryPage />, { wrapper: Providers });
    fireEvent.click(await screen.findByRole("button", { name: "编辑" }));
    expect(screen.getByText(/编辑库存草稿/)).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("备注"), { target: { value: "updated PO" } });
    fireEvent.click(screen.getByRole("button", { name: "保存草稿" }));
    await waitFor(() => expect(requests.some((item) => item.url.endsWith(`/inventory-transactions/${draft.public_id}`) && item.method === "PATCH")).toBe(true));
  });
});
