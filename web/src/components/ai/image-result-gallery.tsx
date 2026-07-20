"use client";

import { useQuery } from "@tanstack/react-query";
import { Image as ImageIcon } from "lucide-react";
import { useEffect } from "react";

import { apiRequest, authenticatedMediaURL } from "@/lib/api";
import type { components } from "@/lib/openapi-types";
import { formatDateTime } from "@/lib/utils";

type ImageResult = components["schemas"]["AIImageResult"];

export function ImageResultGallery({ jobID, language, active, refreshKey, itemLabels = {} }: { jobID: string; language: "zh" | "en"; active: boolean; refreshKey: string; itemLabels?: Record<string, string> }) {
  const zh = language === "zh";
  const query = useQuery({
    queryKey: ["ai-jobs", jobID, "image-results"],
    queryFn: () => apiRequest<{ data: ImageResult[] }>(`/ai-jobs/${encodeURIComponent(jobID)}/image-results`),
    refetchInterval: active ? 2000 : false,
  });
  const { refetch } = query;

  useEffect(() => {
    void refetch();
  }, [refetch, refreshKey]);

  if (query.isLoading) return <section className="animate-pulse rounded-lg border border-border bg-card p-4"><div className="h-5 w-28 rounded bg-muted" /><div className="mt-4 aspect-square max-w-64 rounded bg-muted" /></section>;
  if (query.isError || !query.data?.data.length) return null;

  return <section aria-labelledby="image-results-title" className="space-y-3">
    <div className="flex items-center gap-2"><ImageIcon className="h-4 w-4 text-primary" /><h2 className="font-semibold" id="image-results-title">{zh ? "生成图片" : "Generated images"}</h2></div>
    <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
      {query.data.data.map((result) => <article className="overflow-hidden rounded-lg border border-border bg-card" key={result.public_id}>
        <a className="block bg-muted" href={authenticatedMediaURL(result.media_url)} rel="noreferrer" target="_blank">
          <img alt={zh ? `生成图片候选 ${result.candidate_index}` : `Generated image candidate ${result.candidate_index}`} className="aspect-square w-full object-cover transition-transform duration-200 hover:scale-[1.015] motion-reduce:transition-none" src={authenticatedMediaURL(result.media_url)} />
        </a>
        <div className="space-y-1.5 p-3 text-xs text-muted-foreground">
          {itemLabels[result.job_item_id] ? <p className="font-medium text-foreground">{itemLabels[result.job_item_id]}</p> : null}
          <div className="flex items-center justify-between gap-2"><span>{zh ? `候选 ${result.candidate_index}` : `Candidate ${result.candidate_index}`}</span><span>{result.width} × {result.height}</span></div>
          <p>{formatDateTime(result.created_at)}</p>
        </div>
      </article>)}
    </div>
  </section>;
}
