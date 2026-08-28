import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { LanguageProvider } from "@/lib/i18n";
import AIReferenceSOPsPage from "./page";

function Providers({children}:{children:ReactNode}){return <LanguageProvider><QueryClientProvider client={new QueryClient({defaultOptions:{queries:{retry:false}}})}>{children}</QueryClientProvider></LanguageProvider>}
function response(value:unknown){return Promise.resolve(new Response(JSON.stringify(value),{status:200,headers:{"content-type":"application/json"}}))}
beforeEach(()=>{localStorage.clear();vi.restoreAllMocks()});

describe("AIReferenceSOPsPage",()=>{
 it("separates reference SOP lifecycle and purpose filters from capture SOPs",async()=>{
  vi.spyOn(globalThis,"fetch").mockImplementation((input)=>String(input).includes("/categories")?response({data:[{id:7,name:"手机壳",name_en:"Phone cases"}]}):response({data:[{public_id:"sop-a",category_id:7,category:{id:7,name:"手机壳",name_en:"Phone cases"},versions:[{public_id:"version-a",version_number:1,name_zh:"套机效果",name_en:"Fitted effect",description_zh:"只参考装机比例",description_en:"Proportions only",status:"published",items:[{public_id:"item-a",purpose:"usage_effect"}]}]}]}));
  render(<AIReferenceSOPsPage/>,{wrapper:Providers});
  expect(await screen.findByText("套机效果 · V1")).toBeInTheDocument();
  expect(screen.getByRole("link",{name:"查看"})).toHaveAttribute("href","/ai-reference-sops/sop-a/versions/version-a?mode=view");
  expect(screen.getAllByText("已发布")).toHaveLength(2);
  fireEvent.change(screen.getByLabelText("用途"),{target:{value:"copy_inspiration"}});
  expect(await screen.findByText("当前筛选条件下没有参考 SOP。")).toBeInTheDocument();
 });
});
