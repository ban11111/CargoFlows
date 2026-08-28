package ai

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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
	request.Prompt.OrderedInputListJSON = json.RawMessage(`[]`)
	request.Prompt.TaskBrief = "Generate the specified product image from structured facts without binary references."
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
	prompt, _ := payload["prompt"].(string)
	for _, required := range []string{"[L0 ", "structured facts without binary references", "CASE-17-PRO", "<normalized_input_json>", "<ordered_input_list_json>"} {
		if !strings.Contains(prompt, required) {
			t.Fatalf("Images API prompt missing %q: %s", required, prompt)
		}
	}
}

func TestImagesAPIClientUsesMultipartEditWithSources(t *testing.T) {
	request := responsesImageRequest(t, models.AIExecutionEdit)
	request.Model, request.APIMode = "gpt-image-2", "images"
	request.Mask = &ImageInput{Bytes: []byte("png-mask"), MIMEType: "image/png"}
	expectedPrompt := request.Prompt.ImagesAPIPrompt()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/images/edits" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatal(err)
		}
		if r.FormValue("model") != "gpt-image-2" || len(r.MultipartForm.File["image[]"]) != 3 || len(r.MultipartForm.File["mask"]) != 1 {
			t.Fatalf("form model=%q files=%d masks=%d", r.FormValue("model"), len(r.MultipartForm.File["image[]"]), len(r.MultipartForm.File["mask"]))
		}
		for _, required := range []string{"只按要求编辑图片 1", "图片 1: 编辑底图", "仅修改明确要求的内容", "<ordered_input_list_json>"} {
			if !strings.Contains(r.FormValue("prompt"), required) {
				t.Fatalf("multipart prompt missing %q: %s", required, r.FormValue("prompt"))
			}
		}
		if r.FormValue("prompt") != expectedPrompt {
			t.Fatal("multipart prompt differs from the complete compiled Images API prompt")
		}
		w.Header().Set("x-request-id", "req_edit")
		_, _ = io.WriteString(w, `{"data":[{"b64_json":"YQ=="}]}`)
	}))
	t.Cleanup(server.Close)

	result, err := NewOpenAIImagesClient(server.URL+"/v1", server.Client(), OpenAIImagesConfig{MaxAttempts: 1}).Generate(t.Context(), []byte("sk-images-test"), request)
	if err != nil {
		t.Fatal(err)
	}
	if result.RequestID != "req_edit" || string(result.ImageBytes) != "a" {
		t.Fatalf("result = %#v", result)
	}
}

func TestImagesAPIClientSendsChineseReferenceSOPAndCompositeRequirementsIntact(t *testing.T) {
	snapshot, slot := imagePromptFixture()
	snapshot.ReferenceSOPs = []ReferenceSOPFacts{{PublicID: "sop-a", VersionPublicID: "sop-version-a", VersionNumber: 1, Name: LocalizedNameFacts{ZH: "手机壳装机参考", EN: "Phone case fitted reference"}, Description: LocalizedNameFacts{ZH: "只参考装机关系", EN: "Fitted relationship only"}}}
	snapshot.ExternalReferences = []ExternalReferenceFacts{{PublicID: "external-a", VersionPublicID: "sop-version-a", Purpose: models.AIReferenceUsageEffect, Caption: LocalizedNameFacts{ZH: "另一款手机壳套机", EN: "Another phone case fitted"}, AllowedGuidance: LocalizedNameFacts{ZH: "仅参考装机比例和姿态", EN: "Installed proportion and pose only"}, ForbiddenGuidance: LocalizedNameFacts{ZH: "禁止继承外形、颜色、开孔、品牌、设备、配件和包装", EN: "Do not inherit shape, color, cutouts, brand, device, accessories, or packaging"}, SourceName: "Competitor", SHA256: strings.Repeat("a", 64)}}
	detail := slot
	detail.PublicID = "99999999-9999-4999-8999-999999999999"
	detail.SlotKey = "detail"
	detail.PromptFragment = "必须呈现真实开孔，并保持英文在前、中文在后。"
	slot.CompositeRequirements = []SlotFacts{slot, detail}
	compiled, err := CompileImagePrompt(snapshot, slot, ImageTurnInput{Operation: models.AIExecutionGenerate, ThreadPublicID: "thread-sop"})
	if err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatal(err)
		}
		prompt := r.FormValue("prompt")
		if got := len(r.MultipartForm.File["image[]"]); got != 2 {
			t.Fatalf("binary images = %d, want 2 target-SKU inputs only", got)
		}
		for _, required := range []string{"必须呈现真实开孔", "仅参考装机比例和姿态", "禁止继承外形、颜色、开孔、品牌、设备、配件和包装", `"allowed_guidance":"仅参考装机比例和姿态"`, `"slot_key":"detail"`} {
			if !strings.Contains(prompt, required) {
				t.Fatalf("gpt-image-2 prompt missing %q: %s", required, prompt)
			}
		}
		for _, forbidden := range []string{"Installed proportion and pose only", "Fitted relationship only", "Another phone case fitted"} {
			if strings.Contains(prompt, forbidden) {
				t.Fatalf("gpt-image-2 prompt retained duplicated English SOP guidance %q: %s", forbidden, prompt)
			}
		}
		w.Header().Set("x-request-id", "req_sop_compact")
		_, _ = io.WriteString(w, `{"data":[{"b64_json":"YQ=="}]}`)
	}))
	t.Cleanup(server.Close)

	request := ImageRequest{Model: "gpt-image-2", APIMode: "images", Prompt: compiled, Inputs: []ImageInput{{Bytes: []byte("source-one"), MIMEType: "image/png"}, {Bytes: []byte("source-two"), MIMEType: "image/jpeg"}}}
	if _, err := NewOpenAIImagesClient(server.URL+"/v1", server.Client(), OpenAIImagesConfig{MaxAttempts: 1}).Generate(t.Context(), []byte("sk-images-test"), request); err != nil {
		t.Fatal(err)
	}
}

func TestImagesAPIClientRejectsPromptAboveProviderLimitBeforeSending(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	t.Cleanup(server.Close)

	request := responsesImageRequest(t, models.AIExecutionGenerate)
	request.Model, request.APIMode, request.Inputs = "gpt-image-2", "images", nil
	request.Prompt.Instructions = strings.Repeat("x", maxImagesAPIPromptCharacters+1)
	request.Prompt.TaskBrief = "Generate an image."
	request.Prompt.OrderedInputListJSON = json.RawMessage(`[]`)
	_, err := NewOpenAIImagesClient(server.URL+"/v1", server.Client(), OpenAIImagesConfig{MaxAttempts: 1}).Generate(t.Context(), []byte("sk-images-test"), request)
	if !errors.Is(err, ErrImageProviderPromptTooLong) || called {
		t.Fatalf("err=%v called=%v", err, called)
	}
}
