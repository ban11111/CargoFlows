export function aiFailureMessage(message: string, code: string, zh: boolean) {
  if (!zh) return message;
  if (code === "invalid_input") {
    const details: Record<string, string> = {
      "unsupported job snapshot schema": "任务快照版本不受当前执行器支持。请在升级完成后创建新任务。",
      "malformed job snapshot": "任务快照数据损坏或格式不完整，无法读取。",
      "job snapshot schema mismatch": "任务记录与快照声明的版本不一致。",
      "invalid job output locales": "任务快照中的输出语言策略无效。",
      "malformed slot snapshot": "内容槽位快照损坏或格式不完整。",
      "slot snapshot mismatch": "内容槽位配置与创建任务时冻结的快照不一致。",
      "text prompt compilation failed": "文本 Prompt 编译失败，模板或任务快照中的约束无法组合。",
      "image prompt compilation failed": "图片 Prompt 编译失败，模板或任务快照中的约束无法组合。",
      "selected source asset is unavailable": "所选输入素材已不可用或无法读取。",
      "supplemental source asset is unavailable": "补充输入素材已不可用或无法读取。",
      "frozen brand icon is unavailable": "任务冻结的品牌图标已不可用或无法读取。",
      "frozen structure reference is unavailable": "任务冻结的结构参考图已不可用或无法读取。",
      "frozen style reference is unavailable": "任务冻结的风格参考图已不可用或无法读取。",
      "image item requires selected assets": "图片任务没有可用的已选输入素材。",
    };
    return details[message] ?? `任务输入校验失败：${message}`;
  }
  if (code === "openai_timeout_ambiguous") {
    if (message === "OpenAI call outcome is ambiguous") {
      return "OpenAI 请求已发出，但未收到确认结果。这条旧记录未保存具体是请求超时、连接中断还是 OpenAI 5xx。";
    }
    return "OpenAI 请求发出后超时，未收到响应；结果可能已经产生。";
  }
  if (code === "openai_transport_ambiguous") return "请求发出后，与 OpenAI 的连接中断，未收到响应；结果可能已经产生。";
  if (code === "openai_server_error_ambiguous") {
    const status = message.match(/HTTP (\d{3})/)?.[1];
    return `OpenAI API 返回 HTTP ${status ?? "5xx"}，但未能确认生成结果。`;
  }
  if (code === "openai_prompt_too_long") return "图片 Prompt 超过 OpenAI 的 32,000 字符上限。系统已尝试移除重复说明；仍超限时需精简模板或商品输入。";
  if (code === "openai_model_incompatible" && message === "Selected OpenAI model is incompatible with this image API mode") return "OpenAI 拒绝了图片请求。此历史记录未保留具体无效参数，不能据此断定模型不兼容。";
  return message;
}
