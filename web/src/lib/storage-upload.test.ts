import { describe, expect, it } from "vitest";
import { proxyUploadURL, publicRequestOrigin, storageUpstreamURL } from "./storage-upload";

const signature =
  "X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Credential=test&X-Amz-Date=20260720T000000Z&X-Amz-Expires=900&X-Amz-SignedHeaders=host&X-Amz-Signature=abc";

describe("storage upload proxy", () => {
  it("rewrites a signed loopback MinIO upload to the public app origin", () => {
    const result = proxyUploadURL(
      `http://127.0.0.1:9000/cargoflows/photo-sessions/session/capture.jpg?${signature}`,
      "https://dev.cargoflows.cc",
      "cargoflows",
    );

    expect(result).toBe(
      `https://dev.cargoflows.cc/api/storage/cargoflows/photo-sessions/session/capture.jpg?${signature}`,
    );
  });

  it("does not rewrite unsigned, wrong-bucket, or already-public URLs", () => {
    const unsigned = "http://127.0.0.1:9000/cargoflows/file.jpg";
    const wrongBucket = `http://127.0.0.1:9000/private/file.jpg?${signature}`;
    const publicURL = `https://uploads.example.test/cargoflows/file.jpg?${signature}`;

    expect(proxyUploadURL(unsigned, "https://dev.cargoflows.cc", "cargoflows")).toBe(unsigned);
    expect(proxyUploadURL(wrongBucket, "https://dev.cargoflows.cc", "cargoflows")).toBe(wrongBucket);
    expect(proxyUploadURL(publicURL, "https://dev.cargoflows.cc", "cargoflows")).toBe(publicURL);
  });

  it("builds only signed upstream URLs in the configured source bucket", () => {
    const search = new URLSearchParams(signature);
    const result = storageUpstreamURL(
      ["cargoflows", "photo sessions", "capture.jpg"],
      search,
      "http://127.0.0.1:9000",
      "cargoflows",
    );

    expect(result?.pathname).toBe("/cargoflows/photo%20sessions/capture.jpg");
    expect(result?.host).toBe("127.0.0.1:9000");
    expect(storageUpstreamURL(["private", "file.jpg"], search, "http://127.0.0.1:9000", "cargoflows")).toBeNull();
  });

  it("uses Cloudflare forwarded origin headers", () => {
    const headers = new Headers({ host: "localhost:3005", "x-forwarded-host": "dev.cargoflows.cc", "x-forwarded-proto": "https" });
    expect(publicRequestOrigin(new URL("http://localhost:3005/api/proxy/assets/upload-url"), headers)).toBe(
      "https://dev.cargoflows.cc",
    );
  });
});
