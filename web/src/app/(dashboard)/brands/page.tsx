"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowRight, Shapes, Plus } from "lucide-react";
import Link from "next/link";
import { useState } from "react";
import { apiRequest } from "@/lib/api";
import { useLanguage } from "@/lib/i18n";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";

type Brand = { public_id: string; name: string; icon_count: number; product_count: number };

export default function BrandsPage() {
  const { language } = useLanguage(); const zh = language === "zh"; const queryClient = useQueryClient(); const [name,setName]=useState("");
  const brands=useQuery({queryKey:["brands"],queryFn:()=>apiRequest<{data:Brand[]}>("/brands")});
  const create=useMutation({mutationFn:()=>apiRequest<Brand>("/brands",{method:"POST",body:JSON.stringify({name:name.trim()})}),onSuccess:async()=>{setName("");await queryClient.invalidateQueries({queryKey:["brands"]});}});
  return <div className="space-y-6">
    <header className="grid gap-5 rounded-2xl bg-navy p-6 text-white shadow-[var(--shadow-md)] lg:grid-cols-[1fr_360px] lg:items-end">
      <div><p className="text-[11px] font-bold uppercase tracking-[0.18em] text-[#ff9a68]">CargoFlows · Brand library</p><h1 className="mt-2 text-3xl font-bold tracking-tight sm:text-4xl">{zh?"品牌识别库":"Brand identity library"}</h1><p className="mt-2 max-w-2xl text-sm leading-6 text-white/62">{zh?"集中维护生成任务可以信任的品牌标识版本。图形保持一致，配色随画面风格适配。":"Maintain trusted brand marks for image generation. Geometry stays fixed while color adapts to the composition."}</p></div>
      <form className="flex gap-2 rounded-xl border border-white/10 bg-white/[0.06] p-3" onSubmit={(e)=>{e.preventDefault();if(name.trim())create.mutate();}}><Input className="border-white/15 bg-white/10 text-white placeholder:text-white/35" maxLength={120} onChange={(e)=>setName(e.target.value)} placeholder={zh?"新品牌名称":"New brand name"} value={name}/><Button disabled={!name.trim()||create.isPending}><Plus className="h-4 w-4"/>{zh?"新增":"Add"}</Button></form>
    </header>
    {create.isError?<p className="rounded-md border border-danger/30 bg-danger/5 p-3 text-sm text-danger" role="alert">{zh?"品牌名称已存在或无法创建。":"The brand already exists or could not be created."}</p>:null}
    <section className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">{brands.data?.data.map((brand,index)=><Link className="group rounded-xl border border-border bg-card p-5 shadow-[var(--shadow-sm)] transition hover:-translate-y-0.5 hover:border-primary/35 hover:shadow-[var(--shadow-md)] motion-reduce:transform-none" href={`/brands/${brand.public_id}`} key={brand.public_id}><div className="flex items-start justify-between"><span className="grid h-11 w-11 place-items-center rounded-xl bg-primary/8 font-mono text-xs font-bold text-primary">{String(index+1).padStart(2,"0")}</span><ArrowRight className="h-4 w-4 text-muted-foreground transition group-hover:translate-x-1 group-hover:text-primary motion-reduce:transform-none"/></div><h2 className="mt-5 text-xl font-bold tracking-tight text-navy">{brand.name}</h2><div className="mt-3 flex gap-4 text-xs text-muted-foreground"><span>{brand.icon_count} {zh?"个启用图标":"active marks"}</span><span>{brand.product_count} {zh?"个商品":"products"}</span></div></Link>)}</section>
    {!brands.isLoading&&!brands.data?.data.length?<div className="rounded-xl border border-dashed border-border p-12 text-center"><Shapes className="mx-auto h-7 w-7 text-muted-foreground"/><p className="mt-3 text-sm text-muted-foreground">{zh?"先建立一个品牌，再上传品牌图标。":"Create a brand, then upload its marks."}</p></div>:null}
  </div>;
}
