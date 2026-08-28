import { describe, expect, it } from "vitest";

import { aiFailureMessage } from "./ai-failure-message";

describe("aiFailureMessage", () => {
	it("explains image prompt length failures in Chinese", () => {
		expect(aiFailureMessage("Image prompt exceeds OpenAI's 32,000-character limit", "openai_prompt_too_long", true)).toContain("32,000 字符上限");
	});

	it("does not present legacy generic 400 errors as proven model incompatibility", () => {
		expect(aiFailureMessage("Selected OpenAI model is incompatible with this image API mode", "openai_model_incompatible", true)).toContain("不能据此断定模型不兼容");
	});
  it("explains legacy ambiguous records without claiming an unknown cause", () => {
    expect(aiFailureMessage("OpenAI call outcome is ambiguous", "openai_timeout_ambiguous", true)).toContain("旧记录未保存具体");
  });

  it("localizes the concrete ambiguous failure categories", () => {
    expect(aiFailureMessage("timed out", "openai_timeout_ambiguous", true)).toContain("超时");
    expect(aiFailureMessage("transport", "openai_transport_ambiguous", true)).toContain("连接中断");
    expect(aiFailureMessage("OpenAI image API returned HTTP 503 before a result was confirmed", "openai_server_error_ambiguous", true)).toContain("HTTP 503");
  });

  it("keeps the server message for English", () => {
    expect(aiFailureMessage("provider message", "openai_transport_ambiguous", false)).toBe("provider message");
  });

  it("explains safe input-validation details in Chinese", () => {
    expect(aiFailureMessage("unsupported job snapshot schema", "invalid_input", true)).toContain("任务快照版本");
    expect(aiFailureMessage("image prompt compilation failed", "invalid_input", true)).toContain("图片 Prompt 编译失败");
  });
});
