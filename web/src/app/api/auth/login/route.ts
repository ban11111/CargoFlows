import { cookies } from "next/headers";
import { NextResponse, type NextRequest } from "next/server";

const API_BASE_URL = process.env.API_BASE_URL ?? "http://localhost:8080";

function browserOrigin(request: NextRequest): URL {
  const candidate = request.headers.get("origin") ?? request.headers.get("referer");
  if (candidate) {
    try {
      return new URL(candidate);
    } catch {
      // Fall back to Next's request URL for non-browser clients.
    }
  }
  return request.nextUrl;
}

export async function POST(request: NextRequest) {
  const contentType = request.headers.get("content-type") ?? "";
  const formSubmission = contentType.includes("application/x-www-form-urlencoded");
  const payload = formSubmission
    ? Object.fromEntries((await request.formData()).entries())
    : await request.json();

  const upstream = await fetch(`${API_BASE_URL}/api/v1/auth/login`, {
    method: "POST",
    headers: {
      "content-type": "application/json",
    },
    body: JSON.stringify(payload),
    cache: "no-store",
  });

  const body = await upstream.json().catch(() => ({}));

  if (!upstream.ok) {
    return NextResponse.json(body, { status: upstream.status });
  }

  const token = body.token ?? body.access_token;

  if (typeof token !== "string" || token.length === 0) {
    return NextResponse.json({ message: "Login response did not include a token." }, { status: 502 });
  }

  const cookie = {
    httpOnly: true,
    sameSite: "lax",
    secure: process.env.NODE_ENV === "production",
    path: "/",
    maxAge: 60 * 60 * 24 * 7,
  } as const;

  if (formSubmission) {
    const target = new URL(request.nextUrl.searchParams.get("next") ?? "/skus", browserOrigin(request));
    const response = NextResponse.redirect(target, 303);
    response.cookies.set("cargo_flow_token", token, cookie);
    return response;
  }

  const cookieStore = await cookies();
  cookieStore.set("cargo_flow_token", token, cookie);

  return NextResponse.json({
    user: body.user ?? null,
  });
}
