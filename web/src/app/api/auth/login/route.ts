import { cookies } from "next/headers";
import { NextResponse, type NextRequest } from "next/server";

const API_BASE_URL = process.env.API_BASE_URL ?? "http://localhost:8080";

export async function POST(request: NextRequest) {
  const payload = await request.json();

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

  const cookieStore = await cookies();
  cookieStore.set("cargo_flow_token", token, {
    httpOnly: true,
    sameSite: "lax",
    secure: process.env.NODE_ENV === "production",
    path: "/",
    maxAge: 60 * 60 * 24 * 7,
  });

  return NextResponse.json({
    user: body.user ?? null,
  });
}

