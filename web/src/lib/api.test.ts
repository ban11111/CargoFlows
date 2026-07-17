import { describe, expect, it } from "vitest";

import { authenticatedMediaURL } from "./api";

describe("authenticatedMediaURL", () => {
  it("routes API v1 media through the cookie-authenticated same-origin proxy", () => {
    expect(authenticatedMediaURL("/api/v1/assets/asset-id/media")).toBe("/api/proxy/assets/asset-id/media");
    expect(authenticatedMediaURL("/api/v1/sop-reference-images/image-id/media")).toBe("/api/proxy/sop-reference-images/image-id/media");
  });

  it("leaves non-API image sources unchanged", () => {
    expect(authenticatedMediaURL("/fixture/reference.jpg")).toBe("/fixture/reference.jpg");
    expect(authenticatedMediaURL("https://cdn.example.test/reference.jpg")).toBe("https://cdn.example.test/reference.jpg");
  });
});
