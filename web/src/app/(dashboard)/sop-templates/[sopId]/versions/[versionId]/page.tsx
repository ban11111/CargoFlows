"use client";

import { useQuery } from "@tanstack/react-query";
import { ArrowLeft, RotateCw } from "lucide-react";
import Link from "next/link";
import { useParams, useRouter } from "next/navigation";

import { SOPVersionEditor } from "@/components/sop/sop-version-editor";
import { Button } from "@/components/ui/button";
import { apiRequest } from "@/lib/api";
import { useLanguage } from "@/lib/i18n";
import type { SOPVersion } from "@/lib/sop";

export default function SOPVersionPage() {
  const { language } = useLanguage();
  const params = useParams<{ sopId: string; versionId: string }>();
  const router = useRouter();
  const version = useQuery({ queryKey: ["sop-version", params.versionId], queryFn: () => apiRequest<SOPVersion>(`/sop-versions/${params.versionId}`) });
  if (version.isLoading) return <p className="py-12 text-center text-sm text-muted-foreground" role="status">{language === "zh" ? "正在载入拍摄视图…" : "Loading capture views…"}</p>;
  if (version.isError || !version.data) return <div className="py-12 text-center"><p className="text-sm text-danger">{language === "zh" ? "无法载入这个 SOP 版本。" : "This SOP version could not be loaded."}</p><Button className="mt-4 min-h-11" onClick={() => version.refetch()} variant="secondary"><RotateCw className="h-4 w-4" />{language === "zh" ? "重试" : "Retry"}</Button></div>;
  return <div className="space-y-4"><Button asChild variant="ghost"><Link href="/sop-templates"><ArrowLeft className="h-4 w-4" />{language === "zh" ? "返回 SOP 列表" : "Back to SOP list"}</Link></Button><SOPVersionEditor initialVersion={version.data} key={`${version.data.public_id}-${version.data.updated_at ?? "initial"}`} onVersionChange={(next) => { if (next.public_id !== params.versionId) router.push(`/sop-templates/${next.sop_public_id}/versions/${next.public_id}`); }} /></div>;
}
