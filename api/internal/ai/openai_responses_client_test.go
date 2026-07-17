package ai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"cargoflow/api/internal/models"
)

func responsesTextRequest(t *testing.T) TextRequest {
	t.Helper()
	snapshot, slot := textPromptFixture(models.AIContentSlotTitle)
	prompt, err := CompileTextPrompt(snapshot, slot)
	if err != nil {
		t.Fatal(err)
	}
	return TextRequest{Prompt: prompt, Metadata: map[string]string{"job_id": "77777777-7777-4777-8777-777777777777", "execution_id": "88888888-8888-4888-8888-888888888888"}}
}

func TestResponsesTextClientSendsStrictStoredFalseRequestAndParsesAllOutput(t *testing.T) {
	const apiKey = "sk-test-responses-client-not-real"
	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/responses" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+apiKey {
			t.Errorf("authorization = %q", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Error(err)
		}
		w.Header().Set("x-request-id", "req_test_123")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"resp_test_123","status":"completed","model":"gpt-5.6-terra","output":[{"type":"message","content":[{"type":"output_text","text":"{\"candidates\":["}]},{"type":"message","content":[{"type":"output_text","text":"{\"title\":\"Exact phone case\",\"keywords\":[],\"source_fields\":[\"sku.code\"]},{\"title\":\"Clear phone case\",\"keywords\":[],\"source_fields\":[\"product.name\"]},{\"title\":\"Slim phone case\",\"keywords\":[],\"source_fields\":[\"sku.size\"]}]}"}]}],"usage":{"input_tokens":101,"output_tokens":55,"total_tokens":156,"output_tokens_details":{"reasoning_tokens":7}}}`)
	}))
	t.Cleanup(server.Close)

	client := NewOpenAIResponsesClient(server.URL+"/v1/", server.Client(), OpenAIResponsesConfig{Model: "gpt-5.6-terra", ReasoningEffort: "low", MaxAttempts: 3})
	if client.client.Timeout != 120*time.Second {
		t.Fatalf("client timeout = %s", client.client.Timeout)
	}
	result, err := client.Generate(t.Context(), []byte(apiKey), responsesTextRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	if result.ResponseID != "resp_test_123" || result.RequestID != "req_test_123" || result.Model != "gpt-5.6-terra" || result.Usage.InputTextTokens != 101 || result.Usage.OutputTextTokens != 55 || result.Usage.ReasoningTokens != 7 {
		t.Fatalf("unexpected response: %#v", result)
	}
	var candidates struct {
		Candidates []json.RawMessage `json:"candidates"`
	}
	if err := json.Unmarshal(result.OutputJSON, &candidates); err != nil || len(candidates.Candidates) != 3 {
		t.Fatalf("output = %s, err=%v", result.OutputJSON, err)
	}
	if captured["model"] != "gpt-5.6-terra" || captured["store"] != false || captured["instructions"] == "" || captured["input"] == "" {
		t.Fatalf("request contract missing: %#v", captured)
	}
	reasoning := captured["reasoning"].(map[string]any)
	if reasoning["effort"] != "low" {
		t.Fatalf("reasoning = %#v", reasoning)
	}
	textConfig := captured["text"].(map[string]any)
	format := textConfig["format"].(map[string]any)
	if format["type"] != "json_schema" || format["strict"] != true || format["name"] != "cargoflow_product_title" || format["schema"] == nil {
		t.Fatalf("text format = %#v", format)
	}
	metadata := captured["metadata"].(map[string]any)
	if metadata["job_id"] == "" || metadata["execution_id"] == "" || len(metadata) != 2 {
		t.Fatalf("metadata = %#v", metadata)
	}
}

func TestResponsesTextClientAlwaysAppliesConfiguredTimeoutToClonedClient(t *testing.T) {
	original := &http.Client{Timeout: 10 * time.Minute}
	client := NewOpenAIResponsesClient("https://api.openai.invalid/v1", original, OpenAIResponsesConfig{RequestTimeout: 45 * time.Second})
	if client.client == original || client.client.Timeout != 45*time.Second || original.Timeout != 10*time.Minute {
		t.Fatalf("client=%p original=%p configured=%s original_timeout=%s", client.client, original, client.client.Timeout, original.Timeout)
	}
}

func TestResponsesTextClientRetriesOnlySafeProviderStatuses(t *testing.T) {
	tests := []struct {
		name         string
		firstStatus  int
		wantAttempts int32
		wantError    error
	}{
		{"rate limit", http.StatusTooManyRequests, 2, nil},
		{"server error", http.StatusBadGateway, 2, nil},
		{"authentication", http.StatusUnauthorized, 1, ErrTextProviderAuthentication},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var attempts atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				attempt := attempts.Add(1)
				if attempt == 1 {
					w.Header().Set("Retry-After", "0")
					w.WriteHeader(tc.firstStatus)
					_, _ = io.WriteString(w, `{"error":{"message":"must not leak"}}`)
					return
				}
				_, _ = io.WriteString(w, completedTextResponse(3))
			}))
			t.Cleanup(server.Close)
			client := NewOpenAIResponsesClient(server.URL, server.Client(), OpenAIResponsesConfig{Model: "gpt-5.6-terra", ReasoningEffort: "low", MaxAttempts: 3})
			client.sleep = func(context.Context, time.Duration) error { return nil }
			_, err := client.Generate(t.Context(), []byte("fake-key"), responsesTextRequest(t))
			if !errors.Is(err, tc.wantError) {
				t.Fatalf("error = %v, want %v", err, tc.wantError)
			}
			if attempts.Load() != tc.wantAttempts {
				t.Fatalf("attempts = %d, want %d", attempts.Load(), tc.wantAttempts)
			}
			if err != nil && strings.Contains(err.Error(), "must not leak") {
				t.Fatalf("provider body leaked: %v", err)
			}
		})
	}
}

func TestResponsesTextClientClassifiesRefusalAndInvalidResponses(t *testing.T) {
	tests := []struct {
		name string
		body string
		want error
	}{
		{"refusal", `{"id":"resp","status":"completed","model":"gpt-5.6-terra","output":[{"type":"message","content":[{"type":"refusal","refusal":"no"}]}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2,"output_tokens_details":{"reasoning_tokens":0}}}`, ErrTextProviderRefusal},
		{"incomplete", `{"id":"resp","status":"incomplete","output":[]}`, ErrTextProviderInvalidResponse},
		{"malformed json", `{`, ErrTextProviderInvalidResponse},
		{"trailing json", completedTextResponse(3) + `{}`, ErrTextProviderInvalidResponse},
		{"wrong candidate count", completedTextResponse(1), ErrTextProviderInvalidResponse},
		{"missing model", completedTextResponseWith("", validTitleOutput(3), validUsage()), ErrTextProviderInvalidResponse},
		{"missing usage", completedTextResponseWith("gpt-5.6-terra", validTitleOutput(3), nil), ErrTextProviderInvalidResponse},
		{"inconsistent usage", completedTextResponseWith("gpt-5.6-terra", validTitleOutput(3), map[string]any{"input_tokens": 2, "output_tokens": 3, "total_tokens": 4}), ErrTextProviderInvalidResponse},
		{"negative usage", completedTextResponseWith("gpt-5.6-terra", validTitleOutput(3), map[string]any{"input_tokens": -1, "output_tokens": 3, "total_tokens": 2}), ErrTextProviderInvalidResponse},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, tc.body) }))
			t.Cleanup(server.Close)
			client := NewOpenAIResponsesClient(server.URL, server.Client(), OpenAIResponsesConfig{Model: "gpt-5.6-terra", ReasoningEffort: "low", MaxAttempts: 1})
			_, err := client.Generate(t.Context(), []byte("fake-key"), responsesTextRequest(t))
			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestResponsesTextClientRejectsMalformedTitleCandidates(t *testing.T) {
	tests := []struct {
		name       string
		candidates any
	}{
		{"null candidate", []any{nil, nil, nil}},
		{"missing required field", []any{
			map[string]any{"keywords": []string{}, "source_fields": []string{}},
			map[string]any{"title": "Valid product title", "keywords": []string{}, "source_fields": []string{}},
			map[string]any{"title": "Valid product title", "keywords": []string{}, "source_fields": []string{}},
		}},
		{"wrong field type", []any{
			map[string]any{"title": 123, "keywords": []string{}, "source_fields": []string{}},
			map[string]any{"title": "Valid product title", "keywords": []string{}, "source_fields": []string{}},
			map[string]any{"title": "Valid product title", "keywords": []string{}, "source_fields": []string{}},
		}},
		{"unknown field", []any{
			map[string]any{"title": "Valid product title", "keywords": []string{}, "source_fields": []string{}, "extra": true},
			map[string]any{"title": "Valid product title", "keywords": []string{}, "source_fields": []string{}},
			map[string]any{"title": "Valid product title", "keywords": []string{}, "source_fields": []string{}},
		}},
		{"below minimum length", []any{
			map[string]any{"title": "Too short", "keywords": []string{}, "source_fields": []string{}},
			map[string]any{"title": "Valid product title", "keywords": []string{}, "source_fields": []string{}},
			map[string]any{"title": "Valid product title", "keywords": []string{}, "source_fields": []string{}},
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, completedTextResponseWith("gpt-5.6-terra", map[string]any{"candidates": tc.candidates}, validUsage()))
			}))
			t.Cleanup(server.Close)
			client := NewOpenAIResponsesClient(server.URL, server.Client(), OpenAIResponsesConfig{MaxAttempts: 1})
			_, err := client.Generate(t.Context(), []byte("fake-key"), responsesTextRequest(t))
			if !errors.Is(err, ErrTextProviderInvalidResponse) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

type timeoutRoundTripper struct{ calls atomic.Int32 }

func (transport *timeoutRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	transport.calls.Add(1)
	return nil, timeoutError{}
}

type timeoutError struct{}

func (timeoutError) Error() string   { return "ambiguous timeout containing sensitive body" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }

var _ net.Error = timeoutError{}

func TestResponsesTextClientDoesNotRetryAmbiguousTimeoutOrLeakTransportError(t *testing.T) {
	transport := &timeoutRoundTripper{}
	client := NewOpenAIResponsesClient("https://api.openai.invalid/v1", &http.Client{Transport: transport}, OpenAIResponsesConfig{Model: "gpt-5.6-terra", ReasoningEffort: "low", MaxAttempts: 3})
	_, err := client.Generate(t.Context(), []byte("fake-key"), responsesTextRequest(t))
	if !errors.Is(err, ErrTextProviderAmbiguousTimeout) || transport.calls.Load() != 1 {
		t.Fatalf("error=%v calls=%d", err, transport.calls.Load())
	}
	if strings.Contains(err.Error(), "sensitive body") {
		t.Fatalf("transport error leaked: %v", err)
	}
}

type genericErrorRoundTripper struct{ calls atomic.Int32 }

func (transport *genericErrorRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	transport.calls.Add(1)
	return nil, errors.New("ambiguous transport failure containing sensitive body")
}

func TestResponsesTextClientDoesNotRetryAmbiguousTransportFailure(t *testing.T) {
	transport := &genericErrorRoundTripper{}
	client := NewOpenAIResponsesClient("https://api.openai.invalid/v1", &http.Client{Transport: transport}, OpenAIResponsesConfig{MaxAttempts: 3})
	_, err := client.Generate(t.Context(), []byte("fake-key"), responsesTextRequest(t))
	if !errors.Is(err, ErrTextProviderAmbiguousTransport) || transport.calls.Load() != 1 {
		t.Fatalf("error=%v calls=%d", err, transport.calls.Load())
	}
	if strings.Contains(err.Error(), "sensitive body") {
		t.Fatalf("transport error leaked: %v", err)
	}
}

func TestRetryDelaySupportsProviderHeadersAndCaps(t *testing.T) {
	tests := []struct {
		name   string
		header http.Header
		want   time.Duration
	}{
		{"milliseconds preferred", http.Header{"Retry-After-Ms": []string{"1500"}, "Retry-After": []string{"9"}}, 1500 * time.Millisecond},
		{"fractional seconds", http.Header{"Retry-After": []string{"0.25"}}, 250 * time.Millisecond},
		{"capped", http.Header{"Retry-After-Ms": []string{"60000"}}, 30 * time.Second},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := retryDelay(2, tc.header); got != tc.want {
				t.Fatalf("delay=%s want=%s", got, tc.want)
			}
		})
	}
}

func completedTextResponse(candidateCount int) string {
	return completedTextResponseWith("gpt-5.6-terra", validTitleOutput(candidateCount), validUsage())
}

func validTitleOutput(candidateCount int) map[string]any {
	candidates := make([]map[string]any, candidateCount)
	for index := range candidates {
		candidates[index] = map[string]any{"title": "Valid product title", "keywords": []string{}, "source_fields": []string{"product.name"}}
	}
	return map[string]any{"candidates": candidates}
}

func validUsage() map[string]any {
	return map[string]any{"input_tokens": 1, "output_tokens": 1, "total_tokens": 2, "output_tokens_details": map[string]any{"reasoning_tokens": 0}}
}

func completedTextResponseWith(model string, output any, usage any) string {
	outputJSON, _ := json.Marshal(output)
	response, _ := json.Marshal(map[string]any{
		"id": "resp_ok", "status": "completed", "model": model,
		"output": []any{map[string]any{"type": "message", "content": []any{map[string]any{"type": "output_text", "text": string(outputJSON)}}}},
		"usage":  usage,
	})
	return string(response)
}
