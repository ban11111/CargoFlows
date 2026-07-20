"use client";

import { useEffect } from "react";

import { ErrorNotice } from "@/components/error-notice";
import { useLanguage } from "@/lib/i18n";

export default function DashboardError({ error, reset }: { error: Error & { digest?: string }; reset: () => void }) {
  const { language, t } = useLanguage();
  const zh = language === "zh";

  useEffect(() => {
    // Only the framework digest is shown to the user; the error body stays in server logs.
    console.error("Dashboard route failed", error.digest ?? "no-digest");
  }, [error]);

  return (
    <div className="mx-auto max-w-2xl py-12">
      <ErrorNotice
        actionLabel={t("retry")}
        message={zh ? "页面遇到意外错误，当前操作可能尚未完成。" : "The page encountered an unexpected error and the current action may not have completed."}
        onAction={reset}
        recovery={zh ? "请重试；如果问题持续，请将技术编号发给管理员。" : "Retry. If the problem continues, send the technical ID to an administrator."}
        requestId={error.digest}
        title={zh ? "页面无法继续加载" : "The page could not continue"}
      />
    </div>
  );
}
