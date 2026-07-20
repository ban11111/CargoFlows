import type { Metadata } from "next";
import { QueryProvider } from "@/components/query-provider";
import { LanguageProvider } from "@/lib/i18n";
import "./globals.css";

export const metadata: Metadata = {
  title: {
    default: "CargoFlows · Operations Control",
    template: "%s · CargoFlows",
  },
  description: "Catalog, inventory, capture SOP, asset review, and AI content operations in one flow.",
};

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html lang="zh-CN">
      <body>
        <LanguageProvider>
          <QueryProvider>{children}</QueryProvider>
        </LanguageProvider>
      </body>
    </html>
  );
}
