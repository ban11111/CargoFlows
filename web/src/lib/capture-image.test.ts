import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  CAPTURE_MAX_BYTES,
  CaptureImageError,
  containedImageSize,
  normalizeCaptureImage,
} from "./capture-image";

describe("capture image normalization", () => {
  const drawImage = vi.fn();
  const close = vi.fn();

  beforeEach(() => {
    vi.stubGlobal("createImageBitmap", vi.fn().mockResolvedValue({ width: 8_000, height: 4_000, close }));
    vi.spyOn(HTMLCanvasElement.prototype, "getContext").mockReturnValue({
      fillStyle: "",
      fillRect: vi.fn(),
      drawImage,
    } as unknown as CanvasRenderingContext2D);
    vi.spyOn(HTMLCanvasElement.prototype, "toBlob").mockImplementation((callback) => callback(new Blob(["jpeg"], { type: "image/jpeg" })));
  });

  afterEach(() => {
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
  });

  it("contains landscape and portrait images within 4096 pixels", () => {
    expect(containedImageSize(8_000, 4_000)).toEqual({ width: 4_096, height: 2_048 });
    expect(containedImageSize(2_000, 5_000)).toEqual({ width: 1_638, height: 4_096 });
    expect(containedImageSize(1_200, 800)).toEqual({ width: 1_200, height: 800 });
  });

  it("uses orientation-aware decoding and emits an optimized JPEG", async () => {
    const source = new File(["source"], "phone-photo.heic", { type: "image/heic", lastModified: 123 });
    const result = await normalizeCaptureImage(source);

    expect(createImageBitmap).toHaveBeenCalledWith(source, { imageOrientation: "from-image" });
    expect(drawImage).toHaveBeenCalledWith(expect.anything(), 0, 0, 4_096, 2_048);
    expect(result.file.name).toBe("phone-photo.jpg");
    expect(result.file.type).toBe("image/jpeg");
    expect(result).toMatchObject({ width: 4_096, height: 2_048 });
    expect(close).toHaveBeenCalled();
  });

  it("reports images that cannot be decoded", async () => {
    vi.mocked(createImageBitmap).mockRejectedValueOnce(new Error("bad image"));
    await expect(normalizeCaptureImage(new File(["bad"], "bad.heic"))).rejects.toMatchObject({ code: "decode_failed" });
  });

  it("rejects an optimized result over the backend byte limit", async () => {
    vi.spyOn(HTMLCanvasElement.prototype, "toBlob").mockImplementation((callback) => {
      callback({ size: CAPTURE_MAX_BYTES + 1, type: "image/jpeg" } as Blob);
    });
    await expect(normalizeCaptureImage(new File(["source"], "huge.png"))).rejects.toEqual(new CaptureImageError("too_large"));
  });
});
