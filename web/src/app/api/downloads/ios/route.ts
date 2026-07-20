import { readFile } from "node:fs/promises";
import path from "node:path";
import { NextResponse } from "next/server";

const packages = [
  { fileName: "CargoFlows.ipa", contentType: "application/octet-stream" },
  { fileName: "CargoFlows-iOS-Simulator.zip", contentType: "application/zip" },
] as const;

export const dynamic = "force-dynamic";

export async function GET() {
  for (const packageFile of packages) {
    try {
      const filePath = path.join(process.cwd(), "public", "downloads", packageFile.fileName);
      const file = await readFile(filePath);

      return new NextResponse(new Uint8Array(file), {
        headers: {
          "Cache-Control": "no-store",
          "Content-Disposition": `attachment; filename="${packageFile.fileName}"`,
          "Content-Type": packageFile.contentType,
          "X-Content-Type-Options": "nosniff",
        },
      });
    } catch (error) {
      const code = error && typeof error === "object" && "code" in error ? error.code : undefined;
      if (code !== "ENOENT") throw error;
    }
  }

  return NextResponse.json(
    { error: "The iOS package has not been built yet. Run scripts/package-ios.sh archive or simulator." },
    { status: 404 },
  );
}
