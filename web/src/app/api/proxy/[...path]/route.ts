import { cookies } from "next/headers";
import { NextResponse, type NextRequest } from "next/server";
import { proxyUploadURL, publicRequestOrigin } from "@/lib/storage-upload";

const API_BASE_URL = process.env.API_BASE_URL ?? "http://localhost:8080";
const MINIO_SOURCE_BUCKET = process.env.MINIO_SOURCE_BUCKET ?? "cargoflows";

async function proxy(request: NextRequest, context: { params: Promise<{ path: string[] }> }) {
  const { path } = await context.params;
  const upstreamUrl = new URL(`/api/v1/${path.join("/")}`, API_BASE_URL);

  request.nextUrl.searchParams.forEach((value, key) => {
    upstreamUrl.searchParams.append(key, value);
  });

  const headers = new Headers(request.headers);
  headers.delete("host");
  headers.delete("cookie");

  const token = (await cookies()).get("cargo_flow_token")?.value;
  if (token) {
    headers.set("authorization", `Bearer ${token}`);
  }

  const body = request.method === "GET" || request.method === "HEAD" ? undefined : await request.arrayBuffer();

  const upstream = await fetch(upstreamUrl, {
    method: request.method,
    headers,
    body,
    cache: "no-store",
  });

  const responseHeaders = new Headers(upstream.headers);
  responseHeaders.delete("set-cookie");

  if (upstream.ok && path.at(-1) === "upload-url" && responseHeaders.get("content-type")?.includes("application/json")) {
    const payload = (await upstream.json()) as Record<string, unknown>;
    if (typeof payload.upload_url === "string") {
      payload.upload_url = proxyUploadURL(
        payload.upload_url,
        publicRequestOrigin(request.nextUrl, request.headers),
        MINIO_SOURCE_BUCKET,
      );
    }
    responseHeaders.delete("content-length");
    return NextResponse.json(payload, { status: upstream.status, headers: responseHeaders });
  }

  return new NextResponse(upstream.body, {
    status: upstream.status,
    headers: responseHeaders,
  });
}

export const GET = proxy;
export const POST = proxy;
export const PATCH = proxy;
export const PUT = proxy;
export const DELETE = proxy;
