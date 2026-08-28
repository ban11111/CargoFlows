import { NextResponse } from "next/server";

export const dynamic = "force-dynamic";

export async function GET() {
	const apiBaseURL = process.env.API_BASE_URL ?? "http://localhost:8080";
	try {
		const upstream = await fetch(`${apiBaseURL}/healthz`, { cache: "no-store", signal: AbortSignal.timeout(3000) });
		if (!upstream.ok) {
			return NextResponse.json({ status: "unavailable" }, { status: 503 });
		}
		return NextResponse.json({ status: "ok" }, { headers: { "cache-control": "no-store" } });
	} catch {
		return NextResponse.json({ status: "unavailable" }, { status: 503 });
	}
}
