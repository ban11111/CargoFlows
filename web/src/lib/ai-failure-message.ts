export function aiFailureMessage(message: string, code: string, zh: boolean) {
  if (!zh) return message;
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
  return message;
}
