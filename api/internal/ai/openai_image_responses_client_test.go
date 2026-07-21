package ai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"cargoflows/api/internal/models"
)

func responsesImageRequest(t *testing.T, operation models.AIExecutionOperation) ImageRequest {
	t.Helper()
	snapshot, slot := imagePromptFixture()
	turn := ImageTurnInput{Operation: operation, ThreadPublicID: "thread-a"}
	if operation == models.AIExecutionEdit {
		turn.ParentResultPublicID = "parent-a"
		turn.ParentThreadPublicID = "thread-a"
	}
	prompt, err := CompileImagePrompt(snapshot, slot, turn)
	if err != nil {
		t.Fatal(err)
	}
	inputs := []ImageInput{{MIMEType: "image/png", Bytes: []byte("source-one")}, {MIMEType: "image/jpeg", Bytes: []byte("source-two")}}
	if operation == models.AIExecutionEdit {
		inputs = append([]ImageInput{{MIMEType: "image/png", Bytes: []byte("parent")}}, inputs...)
	}
	return ImageRequest{Prompt: prompt, Inputs: inputs, Metadata: map[string]string{"job_id": "job-public", "execution_id": "execution-public"}}
}

func TestResponsesImageClientSendsStoredFalseToolRequestAndParsesImage(t *testing.T) {
	const apiKey = "sk-test-image-client-not-real"
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
		w.Header().Set("x-request-id", "req_image_123")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"resp_image_123","status":"completed","model":"gpt-5.6","output":[{"type":"image_generation_call","id":"ig_123","status":"completed","result":"aW1hZ2UtYnl0ZXM="}],"usage":{"input_tokens":120,"output_tokens":80,"total_tokens":200,"input_tokens_details":{"image_tokens":70},"output_tokens_details":{"image_tokens":75}}}`)
	}))
	t.Cleanup(server.Close)

	client := NewOpenAIImageResponsesClient(server.URL+"/v1/", server.Client(), OpenAIImageResponsesConfig{Model: "gpt-5.6", MaxAttempts: 2})
	if client.client.Timeout != 180*time.Second {
		t.Fatalf("client timeout = %s", client.client.Timeout)
	}
	result, err := client.Generate(t.Context(), []byte(apiKey), responsesImageRequest(t, models.AIExecutionEdit))
	if err != nil {
		t.Fatal(err)
	}
	if string(result.ImageBytes) != "image-bytes" || result.ResponseID != "resp_image_123" || result.RequestID != "req_image_123" || result.ImageCallID != "ig_123" || result.Model != "gpt-5.6" || result.Usage.InputImageTokens != 70 || result.Usage.OutputImageTokens != 75 || result.Usage.TotalTokens != 200 {
		t.Fatalf("unexpected response: %#v", result)
	}
	if captured["model"] != "gpt-5.6" || captured["store"] != false || captured["instructions"] == "" {
		t.Fatalf("request contract missing: %#v", captured)
	}
	tools := captured["tools"].([]any)
	tool := tools[0].(map[string]any)
	if tool["type"] != "image_generation" || tool["action"] != "edit" || tool["size"] != "1024x1024" || tool["quality"] != "medium" || tool["moderation"] != "auto" {
		t.Fatalf("tool = %#v", tool)
	}
	choice := captured["tool_choice"].(map[string]any)
	if choice["type"] != "image_generation" {
		t.Fatalf("tool choice = %#v", choice)
	}
	input := captured["input"].([]any)
	content := input[0].(map[string]any)["content"].([]any)
	if len(content) != 4 || content[0].(map[string]any)["type"] != "input_text" {
		t.Fatalf("input content = %#v", content)
	}
	inputText := content[0].(map[string]any)["text"].(string)
	for _, required := range []string{"[IMAGE GENERATION TASK BRIEF", "PRIMARY SUBJECT", "Image 1: EDIT BASE", "<normalized_input_json>", "<ordered_input_list_json>"} {
		if !strings.Contains(inputText, required) {
			t.Fatalf("Responses input text missing %q: %s", required, inputText)
		}
	}
	if got := content[1].(map[string]any)["image_url"]; got != "data:image/png;base64,"+base64.StdEncoding.EncodeToString([]byte("parent")) {
		t.Fatalf("first image = %q", got)
	}
	if got := content[2].(map[string]any)["image_url"]; got != "data:image/png;base64,"+base64.StdEncoding.EncodeToString([]byte("source-one")) {
		t.Fatalf("second image = %q", got)
	}
	if got := content[3].(map[string]any)["image_url"]; got != "data:image/jpeg;base64,"+base64.StdEncoding.EncodeToString([]byte("source-two")) {
		t.Fatalf("third image = %q", got)
	}
}

func TestResponsesImageClientRejectsInvalidRequestAndResponse(t *testing.T) {
	client := NewOpenAIImageResponsesClient("https://api.openai.invalid/v1", nil, OpenAIImageResponsesConfig{})
	request := responsesImageRequest(t, models.AIExecutionGenerate)
	request.Inputs[0].MIMEType = "text/html"
	if _, err := client.Generate(t.Context(), []byte("fake"), request); !errors.Is(err, ErrImageProviderInvalidRequest) {
		t.Fatalf("invalid MIME error = %v", err)
	}

	for _, tc := range []struct {
		name string
		body string
	}{
		{"malformed", `{`},
		{"missing image call", `{"id":"resp","status":"completed","model":"gpt-5.6","output":[],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`},
		{"bad base64", `{"id":"resp","status":"completed","model":"gpt-5.6","output":[{"type":"image_generation_call","id":"ig","status":"completed","result":"***"}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`},
		{"multiple image calls", `{"id":"resp","status":"completed","model":"gpt-5.6","output":[{"type":"image_generation_call","id":"ig1","status":"completed","result":"YQ=="},{"type":"image_generation_call","id":"ig2","status":"completed","result":"Yg=="}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`},
		{"impossible image token detail", `{"id":"resp","status":"completed","model":"gpt-5.6","output":[{"type":"image_generation_call","id":"ig","status":"completed","result":"YQ=="}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2,"input_tokens_details":{"image_tokens":2}}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, tc.body) }))
			t.Cleanup(server.Close)
			client := NewOpenAIImageResponsesClient(server.URL, server.Client(), OpenAIImageResponsesConfig{MaxAttempts: 1})
			if _, err := client.Generate(t.Context(), []byte("fake"), responsesImageRequest(t, models.AIExecutionGenerate)); !errors.Is(err, ErrImageProviderInvalidResponse) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestResponsesImageClientUsesPerRequestModel(t *testing.T) {
	client := NewOpenAIImageResponsesClient("https://api.openai.invalid/v1", nil, OpenAIImageResponsesConfig{Model: "fallback-model"})
	request := responsesImageRequest(t, models.AIExecutionGenerate)
	request.Model = "selected-image-host-model"
	body, err := client.requestBody(request)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["model"] != "selected-image-host-model" {
		t.Fatalf("model = %v", decoded["model"])
	}
}

func TestResponsesImageClientRetriesOnlyRateLimits(t *testing.T) {
	for _, tc := range []struct {
		name         string
		status       int
		wantAttempts int32
		want         error
	}{
		{"rate limit", http.StatusTooManyRequests, 2, nil},
		{"server error ambiguous", http.StatusBadGateway, 1, ErrImageProviderAmbiguousTransport},
		{"authentication", http.StatusUnauthorized, 1, ErrImageProviderAuthentication},
		{"moderation", http.StatusBadRequest, 1, ErrImageProviderModeration},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var attempts atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if attempts.Add(1) == 1 {
					w.Header().Set("Retry-After", "0")
					w.WriteHeader(tc.status)
					if tc.name == "moderation" {
						_, _ = io.WriteString(w, `{"error":{"code":"moderation_blocked","message":"must not leak"}}`)
					}
					return
				}
				_, _ = io.WriteString(w, `{"id":"resp","status":"completed","model":"gpt-5.6","output":[{"type":"image_generation_call","id":"ig","status":"completed","result":"YQ=="}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`)
			}))
			t.Cleanup(server.Close)
			client := NewOpenAIImageResponsesClient(server.URL, server.Client(), OpenAIImageResponsesConfig{MaxAttempts: 3})
			client.sleep = func(context.Context, time.Duration) error { return nil }
			_, err := client.Generate(t.Context(), []byte("fake"), responsesImageRequest(t, models.AIExecutionGenerate))
			if !errors.Is(err, tc.want) || attempts.Load() != tc.wantAttempts {
				t.Fatalf("error=%v attempts=%d", err, attempts.Load())
			}
			if err != nil && strings.Contains(err.Error(), "must not leak") {
				t.Fatalf("provider body leaked: %v", err)
			}
		})
	}
}
