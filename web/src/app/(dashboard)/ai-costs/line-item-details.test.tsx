import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { LineItemDetails } from "./page";

const buckets = [
  { public_id: "bucket-1", line_item: "responses", actual_amount_usd: "1.25", status: "synced", project_id: "project-1", api_key_id: "key-1", synced_at: "2026-07-22T00:00:00Z" },
  { public_id: "bucket-2", line_item: "storage", actual_amount_usd: "0.04", status: "synced", project_id: "project-1", api_key_id: "key-1", synced_at: "2026-07-22T00:00:00Z" },
];

describe("LineItemDetails", () => {
  it("starts collapsed and can expand the line-item list", () => {
    render(<LineItemDetails buckets={buckets} />);

    const details = screen.getByText("共 2 项").closest("details");
    expect(details).not.toHaveAttribute("open");

    fireEvent.click(screen.getByText("共 2 项"));
    expect(details).toHaveAttribute("open");
    expect(screen.getByText("responses")).toBeInTheDocument();
    expect(screen.getByText("storage")).toBeInTheDocument();
  });

  it("renders a compact placeholder when there are no line items", () => {
    render(<LineItemDetails buckets={[]} />);

    expect(screen.getByText("—")).toBeInTheDocument();
    expect(document.querySelector("details")).not.toBeInTheDocument();
  });

  it("renders a compact placeholder for a legacy null bucket collection", () => {
    render(<LineItemDetails buckets={null} />);

    expect(screen.getByText("—")).toBeInTheDocument();
    expect(document.querySelector("details")).not.toBeInTheDocument();
  });
});
