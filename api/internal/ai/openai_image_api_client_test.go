package ai

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"cargoflows/api/internal/models"
)

func TestImagesAPIClientUsesGenerationWithoutSources(t *testing.T) {
	var payload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/images/generations" || r.Header.Get("Authorization") != "Bearer sk-images-test" {
			t.Fatalf("request = %s auth=%q", r.URL.Path, r.Header.Get("Authorization"))
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("x-request-id", "req_generation")
		_, _ = io.WriteString(w, `{"data":[{"b64_json":"`+base64.StdEncoding.EncodeToString([]byte("png-bytes"))+`"}],"usage":{"total_tokens":9,"input_tokens":4,"output_tokens":5,"input_tokens_details":{"text_tokens":3,"image_tokens":1}}}`)
	}))
	t.Cleanup(server.Close)

	request := responsesImageRequest(t, models.AIExecutionGenerate)
	request.Model, request.APIMode, request.Inputs = "gpt-image-2", "images", nil
	result, err := NewOpenAIImagesClient(server.URL+"/v1", server.Client(), OpenAIImagesConfig{MaxAttempts: 1}).Generate(t.Context(), []byte("sk-images-test"), request)
	if err != nil {
		t.Fatal(err)
	}
	if result.RequestID != "req_generation" || result.Model != "gpt-image-2" || string(result.ImageBytes) != "png-bytes" || result.Usage.TotalTokens != 9 {
		t.Fatalf("result = %#v", result)
	}
	if payload["model"] != "gpt-image-2" || payload["size"] != "1024x1024" || payload["quality"] != "medium" {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestImagesAPIClientUsesMultipartEditWithSources(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/images/edits" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatal(err)
		}
		if r.FormValue("model") != "gpt-image-2" || len(r.MultipartForm.File["image[]"]) != 2 || len(r.MultipartForm.File["mask"]) != 1 {
			t.Fatalf("form model=%q files=%d masks=%d", r.FormValue("model"), len(r.MultipartForm.File["image[]"]), len(r.MultipartForm.File["mask"]))
		}
		w.Header().Set("x-request-id", "req_edit")
		_, _ = io.WriteString(w, `{"data":[{"b64_json":"YQ=="}]}`)
	}))
	t.Cleanup(server.Close)

	request := responsesImageRequest(t, models.AIExecutionEdit)
	request.Model, request.APIMode = "gpt-image-2", "images"
	request.Mask = &ImageInput{Bytes: []byte("png-mask"), MIMEType: "image/png"}
	result, err := NewOpenAIImagesClient(server.URL+"/v1", server.Client(), OpenAIImagesConfig{MaxAttempts: 1}).Generate(t.Context(), []byte("sk-images-test"), request)
	if err != nil {
		t.Fatal(err)
	}
	if result.RequestID != "req_edit" || string(result.ImageBytes) != "a" {
		t.Fatalf("result = %#v", result)
	}
}
