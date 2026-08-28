import { afterEach, describe, expect, it, vi } from "vitest";
import { GET } from "./route";

describe("production health route", () => {
	afterEach(() => vi.restoreAllMocks());

	it("reports healthy only when the Go API is healthy", async () => {
		const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response('{"status":"ok"}', { status: 200 }));
		const response = await GET();
		expect(response.status).toBe(200);
		expect(await response.json()).toEqual({ status: "ok" });
		expect(fetchMock).toHaveBeenCalledWith("http://localhost:8080/healthz", expect.objectContaining({ cache: "no-store" }));
	});

	it("returns 503 for upstream failures and network errors", async () => {
		vi.spyOn(globalThis, "fetch").mockResolvedValueOnce(new Response("no", { status: 500 }));
		expect((await GET()).status).toBe(503);
		vi.spyOn(globalThis, "fetch").mockRejectedValueOnce(new Error("offline"));
		expect((await GET()).status).toBe(503);
	});
});
