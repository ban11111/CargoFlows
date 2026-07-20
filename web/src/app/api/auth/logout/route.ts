import { NextResponse, type NextRequest } from "next/server";

function browserOrigin(request: NextRequest): URL {
  const candidate = request.headers.get("origin") ?? request.headers.get("referer");
  if (candidate) {
    try {
      return new URL(candidate);
    } catch {
      // Fall through to trusted proxy headers or the internal request URL.
    }
  }

  const forwardedHost = request.headers.get("x-forwarded-host")?.split(",")[0]?.trim();
  if (forwardedHost) {
    const forwardedProtocol = request.headers.get("x-forwarded-proto")?.split(",")[0]?.trim() || "https";
    return new URL(`${forwardedProtocol}://${forwardedHost}`);
  }

  return request.nextUrl;
}

export async function POST(request: NextRequest) {
  const response = NextResponse.redirect(new URL("/login", browserOrigin(request)), 303);
  response.cookies.set("cargo_flow_token", "", {
    httpOnly: true,
    sameSite: "lax",
    secure: process.env.NODE_ENV === "production",
    path: "/",
    maxAge: 0,
  });
  return response;
}
