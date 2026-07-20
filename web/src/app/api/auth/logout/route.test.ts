import { NextRequest } from "next/server";
import { describe, expect, it } from "vitest";

import { POST } from "./route";

describe("POST /api/auth/logout", () => {
  it("clears the authentication cookie and redirects to login", async () => {
    const response = await POST(new NextRequest("http://localhost:3005/api/auth/logout", { method: "POST", headers: { origin: "https://dev.cargoflows.cc" } }));

    expect(response.status).toBe(303);
    expect(response.headers.get("location")).toBe("https://dev.cargoflows.cc/login");
    expect(response.headers.get("set-cookie")).toContain("cargo_flow_token=");
    expect(response.headers.get("set-cookie")).toContain("Max-Age=0");
    expect(response.headers.get("set-cookie")).toContain("Path=/");
    expect(response.headers.get("set-cookie")).toContain("HttpOnly");
  });

  it("uses forwarded origin headers when the browser origin is unavailable", async () => {
    const response = await POST(new NextRequest("http://localhost:3005/api/auth/logout", {
      method: "POST",
      headers: { "x-forwarded-host": "dev.cargoflows.cc", "x-forwarded-proto": "https" },
    }));

    expect(response.headers.get("location")).toBe("https://dev.cargoflows.cc/login");
  });
});
