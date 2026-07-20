import { readFileSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";

describe("global typography", () => {
  it("uses the Apple-style multilingual system stack", () => {
    const css = readFileSync(join(process.cwd(), "src/app/globals.css"), "utf8");
    expect(css).toContain("-apple-system");
    expect(css).toContain("BlinkMacSystemFont");
    expect(css).toContain("PingFang SC");
    expect(css).toContain("Microsoft YaHei");
    expect(css).toContain("font-variant-numeric: tabular-nums");
    expect(css).not.toContain("Arial Narrow");
  });
});
