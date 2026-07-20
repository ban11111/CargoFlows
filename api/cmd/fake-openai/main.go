package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
)

type requestRecord struct {
	Path              string            `json:"path"`
	Store             bool              `json:"store"`
	Metadata          map[string]string `json:"metadata"`
	SchemaName        string            `json:"schema_name"`
	ContainsForbidden bool              `json:"contains_forbidden"`
}

type fakeProvider struct {
	sequence atomic.Int64
	mu       sync.Mutex
	records  []requestRecord
}

func main() {
	provider := &fakeProvider{}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/models", provider.models)
	mux.HandleFunc("POST /v1/responses", provider.responses)
	mux.HandleFunc("GET /__test__/requests", provider.requests)
	mux.HandleFunc("POST /__test__/reset", provider.reset)
	log.Print("local fake OpenAI listening on :8099")
	log.Fatal(http.ListenAndServe(":8099", mux))
}

func (provider *fakeProvider) models(response http.ResponseWriter, request *http.Request) {
	if !validFakeAuthorization(request.Header.Get("Authorization")) {
		http.Error(response, `{"error":{"code":"invalid_api_key"}}`, http.StatusUnauthorized)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"object": "list", "data": []any{}})
}

func (provider *fakeProvider) responses(response http.ResponseWriter, request *http.Request) {
	if !validFakeAuthorization(request.Header.Get("Authorization")) {
		http.Error(response, `{"error":{"code":"invalid_api_key"}}`, http.StatusUnauthorized)
		return
	}
	var body struct {
		Store    bool              `json:"store"`
		Input    string            `json:"input"`
		Metadata map[string]string `json:"metadata"`
		Text     struct {
			Format struct {
				Name   string         `json:"name"`
				Schema map[string]any `json:"schema"`
			} `json:"format"`
		} `json:"text"`
	}
	if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
		http.Error(response, `{"error":{"code":"invalid_request"}}`, http.StatusBadRequest)
		return
	}
	forbidden := strings.Contains(body.Input, "object_key") || strings.Contains(body.Input, "http://") || strings.Contains(body.Input, "https://") || strings.Contains(strings.ToLower(body.Input), "api_key")
	provider.mu.Lock()
	provider.records = append(provider.records, requestRecord{Path: request.URL.Path, Store: body.Store, Metadata: body.Metadata, SchemaName: body.Text.Format.Name, ContainsForbidden: forbidden})
	provider.mu.Unlock()

	count := candidateCount(body.Text.Format.Schema)
	var candidates []map[string]any
	for index := 1; index <= count; index++ {
		if body.Text.Format.Name == "cargoflows_product_title" {
			candidates = append(candidates, map[string]any{
				"title":    fmt.Sprintf("CargoFlows 透明手机壳 CF-CASE-CLR-IP17 候选 %d", index),
				"keywords": []string{"透明手机壳"}, "source_fields": []string{"product.name", "sku.code"},
			})
		} else {
			candidates = append(candidates, map[string]any{
				"short_description": "CargoFlows 透明手机壳，适用于 CF-CASE-CLR-IP17。",
				"selling_points":    []string{"透明外观", "对应指定 SKU"},
				"long_description":  "CargoFlows 透明手机壳，商品编号 CF-CASE-CLR-IP17。内容仅依据已提供的商品资料生成。",
				"search_keywords":   []string{"透明手机壳"}, "source_fields": []string{"product.name", "sku.code"},
			})
		}
	}
	output, _ := json.Marshal(map[string]any{"candidates": candidates})
	id := provider.sequence.Add(1)
	response.Header().Set("x-request-id", fmt.Sprintf("req_fake_%d", id))
	writeJSON(response, http.StatusOK, map[string]any{
		"id": fmt.Sprintf("resp_fake_%d", id), "status": "completed", "model": "fake-responses-model",
		"output": []any{
			map[string]any{
				"type": "message",
				"content": []any{
					map[string]any{"type": "output_text", "text": string(output)},
				},
			},
		},
		"usage": map[string]any{"input_tokens": 80, "output_tokens": 30, "total_tokens": 110, "output_tokens_details": map[string]any{"reasoning_tokens": 4}},
	})
}

func (provider *fakeProvider) requests(response http.ResponseWriter, _ *http.Request) {
	provider.mu.Lock()
	records := append([]requestRecord(nil), provider.records...)
	provider.mu.Unlock()
	writeJSON(response, http.StatusOK, map[string]any{"data": records})
}

func (provider *fakeProvider) reset(response http.ResponseWriter, _ *http.Request) {
	provider.mu.Lock()
	provider.records = nil
	provider.mu.Unlock()
	writeJSON(response, http.StatusOK, map[string]any{"ok": true})
}

func validFakeAuthorization(value string) bool {
	return strings.HasPrefix(value, "Bearer sk-proj-local-e2e-")
}

func candidateCount(schema map[string]any) int {
	properties, _ := schema["properties"].(map[string]any)
	candidates, _ := properties["candidates"].(map[string]any)
	value, _ := candidates["minItems"].(float64)
	if value < 1 || value > 4 {
		return 1
	}
	return int(value)
}

func writeJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(value)
}
