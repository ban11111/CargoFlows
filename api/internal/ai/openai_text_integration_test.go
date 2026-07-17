package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"cargoflow/api/internal/models"
)

func TestOpenAITextExecutorIntegrationUsesSanitizedRequestAndPersistsAudit(t *testing.T) {
	db, leased, setting := prepareTextExecutorLease(t, 2)
	credential := []byte("local-fake-credential-not-real")
	source := &fakeActiveCredentialSource{credential: ActiveOpenAICredential{
		SettingID: setting.ID, KeyFingerprint: setting.KeyFingerprint, APIKey: credential,
	}}

	var calls atomic.Int32
	var capturedBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		if request.Method != http.MethodPost || request.URL.Path != "/v1/responses" {
			t.Errorf("provider request = %s %s", request.Method, request.URL.Path)
		}
		if got := request.Header.Get("Authorization"); got != "Bearer local-fake-credential-not-real" {
			t.Errorf("authorization = %q", got)
		}
		capturedBody, _ = io.ReadAll(request.Body)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("x-request-id", "req_integration")
		_, _ = io.WriteString(w, completedTextResponseWith("fake-responses-model", map[string]any{
			"candidates": []any{
				map[string]any{"title": "Protective phone case CASE-001", "keywords": []string{"phone case"}, "source_fields": []string{"product.name", "sku.code"}},
				map[string]any{"title": "CASE-001 protective phone case", "keywords": []string{"protective case"}, "source_fields": []string{"sku.code", "product.name"}},
			},
		}, map[string]any{"input_tokens": 80, "output_tokens": 30, "total_tokens": 110, "output_tokens_details": map[string]any{"reasoning_tokens": 4}}))
	}))
	t.Cleanup(server.Close)

	provider := NewOpenAIResponsesClient(server.URL+"/v1", server.Client(), OpenAIResponsesConfig{
		Model: "fake-responses-model", ReasoningEffort: "low", MaxAttempts: 1,
	})
	executor := newTextExecutorWithClock(db, source, provider, TextExecutorConfig{
		Model: "fake-responses-model", ReasoningEffort: "low",
	}, fixedClock{now: time.Date(2026, 7, 17, 10, 0, 10, 0, time.UTC)})
	if err := executor.Execute(t.Context(), leased); err != nil {
		t.Fatal(err)
	}

	if calls.Load() != 1 {
		t.Fatalf("provider calls = %d", calls.Load())
	}
	if !bytes.Equal(credential, make([]byte, len(credential))) {
		t.Fatal("decrypted credential was not cleared")
	}
	if bytes.Contains(capturedBody, []byte("local-fake-credential")) || bytes.Contains(capturedBody, []byte("object_key")) || bytes.Contains(capturedBody, []byte("http://")) || bytes.Contains(capturedBody, []byte("https://")) {
		t.Fatalf("provider body contains a credential or private asset locator: %s", capturedBody)
	}
	var requestBody struct {
		Store    bool              `json:"store"`
		Metadata map[string]string `json:"metadata"`
	}
	if err := json.Unmarshal(capturedBody, &requestBody); err != nil {
		t.Fatal(err)
	}
	if requestBody.Store || len(requestBody.Metadata) != 3 {
		t.Fatalf("unsafe request settings: %#v", requestBody)
	}
	for key, value := range requestBody.Metadata {
		if !strings.HasSuffix(key, "_id") || value == "" {
			t.Fatalf("metadata must contain public identifiers only: %#v", requestBody.Metadata)
		}
	}

	var execution models.AIExecution
	if err := db.First(&execution).Error; err != nil {
		t.Fatal(err)
	}
	if execution.Status != models.AIExecutionCompleted || execution.OpenAIResponseID == "" || execution.OpenAIRequestID != "req_integration" || execution.TotalTokens != 110 {
		t.Fatalf("execution = %#v", execution)
	}
	var results []models.AITextResult
	if err := db.Order("candidate_index").Find(&results).Error; err != nil || len(results) != 2 {
		t.Fatalf("results=%#v err=%v", results, err)
	}
	var ledgers int64
	if err := db.Model(&models.AIUsageLedger{}).Where("open_ai_request_id = ?", "req_integration").Count(&ledgers).Error; err != nil || ledgers != 1 {
		t.Fatalf("usage ledgers=%d err=%v", ledgers, err)
	}
}

func TestKindRoutingExecutorNeverCallsTextProviderForImageSlots(t *testing.T) {
	db, _, items := seedQueueItems(t, 1)
	if err := db.Model(&models.AIJobItem{}).Where("id = ?", items[0].ID).Update("kind", models.AIContentSlotImage).Error; err != nil {
		t.Fatal(err)
	}
	leased, err := NewQueue(db).LeaseNext(t.Context(), "worker-image-integration", time.Now().UTC(), time.Minute)
	if err != nil || leased == nil {
		t.Fatalf("lease=%#v err=%v", leased, err)
	}
	var providerCalls atomic.Int32
	text := NewTextExecutor(db, &fakeActiveCredentialSource{}, textProviderFunc(func(context.Context, []byte, TextRequest) (TextResponse, error) {
		providerCalls.Add(1)
		return TextResponse{}, nil
	}), TextExecutorConfig{})
	router := NewKindRoutingExecutor(false, NewDryRunExecutor(db), text)
	if err := router.Execute(t.Context(), *leased); err == nil {
		t.Fatal("real image execution must fail safely until the image executor is implemented")
	}
	if providerCalls.Load() != 0 {
		t.Fatalf("image slot made %d text-provider calls", providerCalls.Load())
	}
}
