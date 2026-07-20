import { cookies } from "next/headers";
import { NextResponse, type NextRequest } from "next/server";

const API_BASE_URL = process.env.API_BASE_URL ?? "http://localhost:8080";

export async function POST(request: NextRequest) {
  const cookieStore = await cookies();
  const currentToken = cookieStore.get("cargo_flow_token")?.value;
  if (!currentToken) {
    return NextResponse.json({ code: "unauthorized", message: "Authentication required." }, { status: 401 });
  }

  const upstream = await fetch(`${API_BASE_URL}/api/v1/auth/change-password`, {
    method: "POST",
    headers: { "content-type": "application/json", authorization: `Bearer ${currentToken}` },
    body: JSON.stringify(await request.json()),
    cache: "no-store",
  });
  const body = await upstream.json().catch(() => ({}));
  if (!upstream.ok) return NextResponse.json(body, { status: upstream.status });
  if (typeof body.token !== "string" || !body.token) {
    return NextResponse.json({ message: "Password change response did not include a token." }, { status: 502 });
  }

  const response = NextResponse.json({ user: body.user });
  response.cookies.set("cargo_flow_token", body.token, {
    httpOnly: true,
    sameSite: "lax",
    secure: process.env.NODE_ENV === "production",
    path: "/",
    maxAge: 60 * 60 * 24 * 7,
  });
  return response;
}
