package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSmokeRequiresExplicitOptInAndKey(t *testing.T) {
	getenv := func(string) string { return "" }
	if err := run(t.Context(), getenv, io.Discard, nil, false); err != errSmokeDisabled {
		t.Fatalf("disabled error = %v", err)
	}
	getenv = func(name string) string {
		if name == "OPENAI_SMOKE_TEST" {
			return "1"
		}
		return ""
	}
	if err := run(t.Context(), getenv, io.Discard, nil, false); err == nil || !strings.Contains(err.Error(), "OPENAI_API_KEY") {
		t.Fatalf("missing-key error = %v", err)
	}
}

func TestSmokePrintsOnlyProviderIdentifiersAndUsage(t *testing.T) {
	const fakeKey = "local-smoke-credential-not-real"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer "+fakeKey {
			t.Error("missing fake bearer credential")
		}
		w.Header().Set("x-request-id", "req_smoke")
		_, _ = io.WriteString(w, `{"id":"resp_smoke","status":"completed","model":"fake-model","output":[{"type":"message","content":[{"type":"output_text","text":"{\"candidates\":[{\"title\":\"CargoFlow Test Protective Phone Case SMOKE-CASE-001\",\"keywords\":[\"phone case\"],\"source_fields\":[\"product.name\",\"sku.code\"]}]}"}]}],"usage":{"input_tokens":20,"output_tokens":10,"total_tokens":30,"output_tokens_details":{"reasoning_tokens":2}}}`)
	}))
	t.Cleanup(server.Close)
	values := map[string]string{
		"OPENAI_SMOKE_TEST": "1", "OPENAI_API_KEY": fakeKey, "OPENAI_BASE_URL": server.URL, "OPENAI_TEXT_MODEL": "fake-model",
	}
	var output bytes.Buffer
	if err := run(context.Background(), func(name string) string { return values[name] }, &output, server.Client(), true); err != nil {
		t.Fatal(err)
	}
	text := output.String()
	for _, expected := range []string{"response_id=resp_smoke", "request_id=req_smoke", "model=fake-model", "total_tokens=30"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("output %q missing %q", text, expected)
		}
	}
	for _, forbidden := range []string{fakeKey, "CargoFlow Test", "SMOKE-CASE-001", "candidates", "instructions"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("smoke output leaked %q: %q", forbidden, text)
		}
	}
}

func TestSmokeRejectsNonOfficialProviderAndUnsafeOutputMetadata(t *testing.T) {
	values := map[string]string{"OPENAI_SMOKE_TEST": "1", "OPENAI_API_KEY": "local-key", "OPENAI_BASE_URL": "https://attacker.invalid/v1"}
	if err := run(t.Context(), func(name string) string { return values[name] }, io.Discard, nil, false); err == nil || !strings.Contains(err.Error(), "official") {
		t.Fatalf("non-official base URL error = %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("x-request-id", "req_safe")
		_, _ = io.WriteString(w, `{"id":"resp_safe","status":"completed","model":"fake-model\ninjected","output":[{"type":"message","content":[{"type":"output_text","text":"{\"candidates\":[{\"title\":\"CargoFlow Test Protective Phone Case SMOKE-CASE-001\",\"keywords\":[],\"source_fields\":[\"product.name\"]}]}"}]}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2,"output_tokens_details":{"reasoning_tokens":0}}}`)
	}))
	t.Cleanup(server.Close)
	values["OPENAI_BASE_URL"] = server.URL
	values["OPENAI_TEXT_MODEL"] = "fake-model"
	if err := run(t.Context(), func(name string) string { return values[name] }, io.Discard, server.Client(), true); err == nil || !strings.Contains(err.Error(), "unsafe identifier") {
		t.Fatalf("unsafe metadata error = %v", err)
	}
}
