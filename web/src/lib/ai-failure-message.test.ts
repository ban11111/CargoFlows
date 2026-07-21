import { describe, expect, it } from "vitest";

import { aiFailureMessage } from "./ai-failure-message";

describe("aiFailureMessage", () => {
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
});
