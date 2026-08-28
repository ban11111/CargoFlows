import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { LanguageProvider } from "@/lib/i18n";
import AIReferenceSOPEditor from "./page";

function Providers({children}:{children:ReactNode}){return <LanguageProvider><QueryClientProvider client={new QueryClient({defaultOptions:{queries:{retry:false}}})}>{children}</QueryClientProvider></LanguageProvider>}
function response(value:unknown){return Promise.resolve(new Response(JSON.stringify(value),{status:200,headers:{"content-type":"application/json"}}))}

beforeEach(()=>{localStorage.clear();vi.restoreAllMocks()});

describe("AIReferenceSOPEditor",()=>{
 it("explains how each purpose limits image-model inheritance",async()=>{
  vi.spyOn(globalThis,"fetch").mockImplementation(()=>response({public_id:"version-a",version_number:1,name_zh:"参考",name_en:"Reference",description_zh:"说明",description_en:"Description",status:"draft",items:[]}));
  await act(async()=>{render(<AIReferenceSOPEditor params={Promise.resolve({sopId:"sop-a",versionId:"version-a"})} searchParams={Promise.resolve({})}/>,{wrapper:Providers});});
  const purpose=await screen.findByLabelText("参考用途");
  expect(screen.getByText("可参考姿态、空间关系、装机比例和场景；图中的商品会被视为非目标占位主体。")).toBeInTheDocument();
  expect(screen.getByPlaceholderText("例如：禁止继承来源手机壳的外形、颜色、开孔、品牌、设备、配件和包装")).toBeInTheDocument();
  fireEvent.change(purpose,{target:{value:"copy_inspiration"}});
  expect(screen.getByText("只用于标题、SEO 等文字生成，不会发送给图片模型。")).toBeInTheDocument();
  fireEvent.change(purpose,{target:{value:"visual_style"}});
  expect(screen.getByText("只影响背景、光线、色调、构图和氛围；必须上传排除来源商品的蒙版。")).toBeInTheDocument();
 });

 it("shows a version as read-only when opened in view mode",async()=>{
  vi.spyOn(globalThis,"fetch").mockImplementation(()=>response({public_id:"version-a",version_number:2,name_zh:"套机效果",name_en:"Fitted effect",description_zh:"只参考装机比例",description_en:"Proportions only",status:"published",items:[]}));
  await act(async()=>{render(<AIReferenceSOPEditor params={Promise.resolve({sopId:"sop-a",versionId:"version-a"})} searchParams={Promise.resolve({mode:"view"})}/>,{wrapper:Providers});});
  expect(await screen.findByRole("heading",{name:"查看 AI 参考 SOP · V2"})).toBeInTheDocument();
  expect(screen.getByText("当前为只读查看模式。")).toBeInTheDocument();
  expect(screen.getByText("套机效果")).toBeInTheDocument();
  expect(screen.queryByRole("button",{name:"保存基本信息"})).not.toBeInTheDocument();
  expect(screen.queryByText("添加参考图片")).not.toBeInTheDocument();
 });
});
