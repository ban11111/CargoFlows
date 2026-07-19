import { NextResponse, type NextRequest } from "next/server";
import { storageUpstreamURL } from "@/lib/storage-upload";

const MINIO_UPLOAD_BASE_URL = process.env.MINIO_UPLOAD_BASE_URL ?? "http://127.0.0.1:9000";
const MINIO_SOURCE_BUCKET = process.env.MINIO_SOURCE_BUCKET ?? "cargoflow";

export const dynamic = "force-dynamic";

export async function PUT(request: NextRequest, context: { params: Promise<{ path: string[] }> }) {
  const { path } = await context.params;
  const upstreamURL = storageUpstreamURL(
    path,
    request.nextUrl.searchParams,
    MINIO_UPLOAD_BASE_URL,
    MINIO_SOURCE_BUCKET,
  );

  if (!upstreamURL) {
    return NextResponse.json({ error: "A valid signed source upload URL is required." }, { status: 403 });
  }

  const headers = new Headers();
  const contentType = request.headers.get("content-type");
  if (contentType) headers.set("content-type", contentType);

  const upstream = await fetch(upstreamURL, {
    method: "PUT",
    headers,
    body: await request.arrayBuffer(),
    cache: "no-store",
  });

  const responseHeaders = new Headers();
  for (const header of ["content-type", "etag", "x-amz-request-id", "x-minio-deployment-id"]) {
    const value = upstream.headers.get(header);
    if (value) responseHeaders.set(header, value);
  }
  responseHeaders.set("cache-control", "no-store");

  return new NextResponse(upstream.body, { status: upstream.status, headers: responseHeaders });
}
