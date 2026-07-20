export const CAPTURE_MAX_PIXEL_DIMENSION = 4_096;
export const CAPTURE_JPEG_QUALITY = 0.82;
export const CAPTURE_MAX_BYTES = 50 * 1024 * 1024;

export type CaptureImageErrorCode = "decode_failed" | "canvas_failed" | "too_large";

export class CaptureImageError extends Error {
  constructor(public readonly code: CaptureImageErrorCode) {
    super(code);
  }
}

export function containedImageSize(
  width: number,
  height: number,
  maxDimension = CAPTURE_MAX_PIXEL_DIMENSION,
): { width: number; height: number } {
  if (!Number.isFinite(width) || !Number.isFinite(height) || width <= 0 || height <= 0) {
    throw new CaptureImageError("decode_failed");
  }
  const scale = Math.min(1, maxDimension / Math.max(width, height));
  return {
    width: Math.max(1, Math.round(width * scale)),
    height: Math.max(1, Math.round(height * scale)),
  };
}

type DecodedImage = CanvasImageSource & { width: number; height: number; close?: () => void };

async function decodeWithImageElement(file: File): Promise<DecodedImage> {
  const url = URL.createObjectURL(file);
  try {
    const image = new Image();
    image.decoding = "async";
    image.src = url;
    await image.decode();
    return image;
  } catch {
    throw new CaptureImageError("decode_failed");
  } finally {
    URL.revokeObjectURL(url);
  }
}

async function decodeImage(file: File): Promise<DecodedImage> {
  if (typeof createImageBitmap === "function") {
    try {
      return await createImageBitmap(file, { imageOrientation: "from-image" });
    } catch {
      throw new CaptureImageError("decode_failed");
    }
  }
  return decodeWithImageElement(file);
}

function canvasJPEG(canvas: HTMLCanvasElement): Promise<Blob> {
  return new Promise((resolve, reject) => {
    canvas.toBlob((blob) => {
      if (blob) resolve(blob);
      else reject(new CaptureImageError("canvas_failed"));
    }, "image/jpeg", CAPTURE_JPEG_QUALITY);
  });
}

export interface NormalizedCaptureImage {
  file: File;
  width: number;
  height: number;
}

export async function normalizeCaptureImage(source: File): Promise<NormalizedCaptureImage> {
  const image = await decodeImage(source);
  try {
    const size = containedImageSize(image.width, image.height);
    const canvas = document.createElement("canvas");
    canvas.width = size.width;
    canvas.height = size.height;
    const context = canvas.getContext("2d");
    if (!context) throw new CaptureImageError("canvas_failed");

    context.fillStyle = "#ffffff";
    context.fillRect(0, 0, size.width, size.height);
    context.drawImage(image, 0, 0, size.width, size.height);
    const blob = await canvasJPEG(canvas);
    if (blob.size > CAPTURE_MAX_BYTES) throw new CaptureImageError("too_large");

    const baseName = source.name.replace(/\.[^.]+$/, "").trim() || "capture";
    return {
      file: new File([blob], `${baseName}.jpg`, { type: "image/jpeg", lastModified: Date.now() }),
      ...size,
    };
  } finally {
    image.close?.();
  }
}
