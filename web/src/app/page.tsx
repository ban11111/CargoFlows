import type { Metadata } from "next";
import { LandingPage } from "@/components/landing/landing-page";

const defaultPublicAppUrl = "https://dev.cargoflows.cc";

export const metadata: Metadata = {
  title: "CargoFlows for iOS",
  description: "在 iPhone 上完成 SKU 查询、库存调整与 SOP 引导拍摄。",
};

export default function HomePage() {
  const publicAppUrl = process.env.NEXT_PUBLIC_PUBLIC_APP_URL ?? defaultPublicAppUrl;

  return <LandingPage publicAppUrl={publicAppUrl} />;
}
