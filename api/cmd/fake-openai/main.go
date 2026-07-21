package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
)

type requestRecord struct {
	Path               string            `json:"path"`
	Store              bool              `json:"store"`
	Metadata           map[string]string `json:"metadata"`
	SchemaName         string            `json:"schema_name"`
	ContainsForbidden  bool              `json:"contains_forbidden"`
	Model              string            `json:"model,omitempty"`
	Action             string            `json:"action,omitempty"`
	InputCount         int               `json:"input_count,omitempty"`
	MaskPresent        bool              `json:"mask_present,omitempty"`
	HasGeneratedParent bool              `json:"has_generated_parent,omitempty"`
}

type fakeProvider struct {
	sequence atomic.Int64
	mu       sync.Mutex
	records  []requestRecord
}

var (
	fakePNGOnce sync.Once
	fakePNGData string
)

func main() {
	provider := &fakeProvider{}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/models", provider.models)
	mux.HandleFunc("POST /v1/responses", provider.responses)
	mux.HandleFunc("POST /v1/images/generations", provider.imageGeneration)
	mux.HandleFunc("POST /v1/images/edits", provider.imageEdit)
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
	writeJSON(response, http.StatusOK, map[string]any{"object": "list", "data": []any{
		map[string]any{"id": "gpt-5.6", "object": "model"},
		map[string]any{"id": "gpt-5.6-terra", "object": "model"},
		map[string]any{"id": "gpt-image-2", "object": "model"},
	}})
}

func (provider *fakeProvider) responses(response http.ResponseWriter, request *http.Request) {
	if !validFakeAuthorization(request.Header.Get("Authorization")) {
		http.Error(response, `{"error":{"code":"invalid_api_key"}}`, http.StatusUnauthorized)
		return
	}
	var body struct {
		Model    string            `json:"model"`
		Store    bool              `json:"store"`
		Input    json.RawMessage   `json:"input"`
		Metadata map[string]string `json:"metadata"`
		Tools    []struct {
			Type   string `json:"type"`
			Action string `json:"action"`
		} `json:"tools"`
		Text struct {
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
	if len(body.Tools) == 1 && body.Tools[0].Type == "image_generation" {
		provider.imageResponse(response, body.Model, body.Store, body.Metadata, body.Tools[0].Action, body.Input)
		return
	}
	if len(body.Input) == 0 || !json.Valid(body.Input) {
		http.Error(response, `{"error":{"code":"invalid_request"}}`, http.StatusBadRequest)
		return
	}
	forbidden := containsForbidden(string(body.Input))
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

func (provider *fakeProvider) imageResponse(response http.ResponseWriter, model string, store bool, metadata map[string]string, action string, input json.RawMessage) {
	inputText := string(input)
	record := requestRecord{Path: "/v1/responses", Store: store, Metadata: metadata, Model: model, Action: action, InputCount: strings.Count(inputText, `"type":"input_image"`), HasGeneratedParent: strings.Contains(inputText, `"kind":"generated_parent"`) || strings.Contains(inputText, `\"kind\":\"generated_parent\"`), ContainsForbidden: containsForbidden(inputText)}
	provider.appendRecord(record)
	id := provider.sequence.Add(1)
	response.Header().Set("x-request-id", fmt.Sprintf("req_fake_image_%d", id))
	writeJSON(response, http.StatusOK, map[string]any{
		"id": fmt.Sprintf("resp_fake_image_%d", id), "status": "completed", "model": model,
		"output": []any{map[string]any{"type": "image_generation_call", "id": fmt.Sprintf("ig_fake_%d", id), "status": "completed", "result": fakePNGBase64()}},
		"usage":  map[string]any{"input_tokens": 12, "output_tokens": 8, "total_tokens": 20, "input_tokens_details": map[string]any{"image_tokens": 4}, "output_tokens_details": map[string]any{"image_tokens": 8}},
	})
}

func (provider *fakeProvider) imageGeneration(response http.ResponseWriter, request *http.Request) {
	if !validFakeAuthorization(request.Header.Get("Authorization")) {
		http.Error(response, `{"error":{"code":"invalid_api_key"}}`, http.StatusUnauthorized)
		return
	}
	var body struct{ Model, Prompt, Size string }
	if json.NewDecoder(request.Body).Decode(&body) != nil || body.Model == "" || body.Prompt == "" {
		http.Error(response, `{"error":{"code":"invalid_request"}}`, http.StatusBadRequest)
		return
	}
	provider.appendRecord(requestRecord{Path: request.URL.Path, Model: body.Model, Action: "generate", ContainsForbidden: containsForbidden(body.Prompt)})
	provider.writeImageAPIResponse(response, body.Model)
}

func (provider *fakeProvider) imageEdit(response http.ResponseWriter, request *http.Request) {
	if !validFakeAuthorization(request.Header.Get("Authorization")) {
		http.Error(response, `{"error":{"code":"invalid_api_key"}}`, http.StatusUnauthorized)
		return
	}
	if request.ParseMultipartForm(64<<20) != nil || request.FormValue("model") == "" || request.FormValue("prompt") == "" {
		http.Error(response, `{"error":{"code":"invalid_request"}}`, http.StatusBadRequest)
		return
	}
	provider.appendRecord(requestRecord{Path: request.URL.Path, Model: request.FormValue("model"), Action: "edit", InputCount: len(request.MultipartForm.File["image[]"]), MaskPresent: len(request.MultipartForm.File["mask"]) == 1, ContainsForbidden: containsForbidden(request.FormValue("prompt"))})
	provider.writeImageAPIResponse(response, request.FormValue("model"))
}

func (provider *fakeProvider) writeImageAPIResponse(response http.ResponseWriter, model string) {
	id := provider.sequence.Add(1)
	response.Header().Set("x-request-id", fmt.Sprintf("req_fake_image_%d", id))
	writeJSON(response, http.StatusOK, map[string]any{"model": model, "data": []any{map[string]any{"b64_json": fakePNGBase64()}}, "usage": map[string]any{"input_tokens": 12, "output_tokens": 8, "total_tokens": 20, "input_tokens_details": map[string]any{"image_tokens": 4}}})
}

func (provider *fakeProvider) appendRecord(record requestRecord) {
	provider.mu.Lock()
	provider.records = append(provider.records, record)
	provider.mu.Unlock()
}

func containsForbidden(value string) bool {
	lower := strings.ToLower(value)
	return strings.Contains(lower, "object_key") || strings.Contains(lower, "http://") || strings.Contains(lower, "https://") || strings.Contains(lower, "api_key")
}

func fakePNGBase64() string {
	fakePNGOnce.Do(func() {
		canvas := image.NewNRGBA(image.Rect(0, 0, 1024, 1024))
		for y := 0; y < 1024; y++ {
			for x := 0; x < 1024; x++ {
				canvas.SetNRGBA(x, y, color.NRGBA{R: uint8(40 + x%80), G: uint8(90 + y%80), B: 160, A: 255})
			}
		}
		var encoded bytes.Buffer
		_ = png.Encode(&encoded, canvas)
		fakePNGData = base64.StdEncoding.EncodeToString(encoded.Bytes())
	})
	return fakePNGData
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
