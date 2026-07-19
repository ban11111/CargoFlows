import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { LanguageProvider } from "@/lib/i18n";
import { LandingPage } from "./landing-page";

describe("LandingPage", () => {
  it("exposes the iOS download and switches all fixed copy to English", () => {
    render(
      <LanguageProvider>
        <LandingPage publicAppUrl="https://example.trycloudflare.com" />
      </LanguageProvider>,
    );

    expect(screen.getByRole("heading", { name: "让每次拍摄，都准确到位。" })).toBeInTheDocument();
    const downloadLinks = screen.getAllByRole("link", { name: /下载 iOS App/ });
    expect(downloadLinks).toHaveLength(2);
    for (const link of downloadLinks) {
      expect(link).toHaveAttribute("href", "/api/downloads/ios");
    }

    fireEvent.click(screen.getByRole("button", { name: "语言" }));

    expect(screen.getByRole("heading", { name: "Every capture, exactly right." })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /Cloudflare development tunnel connected/ })).toHaveAttribute(
      "href",
      "https://example.trycloudflare.com",
    );
  });
});
