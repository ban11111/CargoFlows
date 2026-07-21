package main

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFakeProviderSupportsSanitizedImageResponsesAndMaskedEdits(t *testing.T) {
	provider := &fakeProvider{}

	responsesBody := []byte(`{"model":"gpt-5.6","store":false,"metadata":{"job_id":"job-public"},"input":[{"role":"user","content":[{"type":"input_text","text":"{\"ordered_inputs\":[{\"kind\":\"generated_parent\"}]}"},{"type":"input_image","image_url":"data:image/png;base64,YQ=="}]}],"tools":[{"type":"image_generation","action":"edit"}]}`)
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(responsesBody))
	request.Header.Set("Authorization", "Bearer sk-proj-local-e2e-image")
	response := httptest.NewRecorder()
	provider.responses(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("responses status=%d body=%s", response.Code, response.Body.String())
	}

	var editBody bytes.Buffer
	writer := multipart.NewWriter(&editBody)
	_ = writer.WriteField("model", "gpt-image-2")
	_ = writer.WriteField("prompt", "safe edit")
	imagePart, _ := writer.CreateFormFile("image[]", "source.png")
	_, _ = imagePart.Write([]byte("source"))
	maskPart, _ := writer.CreateFormFile("mask", "mask.png")
	_, _ = maskPart.Write([]byte("mask"))
	_ = writer.Close()
	request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", &editBody)
	request.Header.Set("Authorization", "Bearer sk-proj-local-e2e-image")
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response = httptest.NewRecorder()
	provider.imageEdit(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("images edit status=%d body=%s", response.Code, response.Body.String())
	}

	provider.mu.Lock()
	records := append([]requestRecord(nil), provider.records...)
	provider.mu.Unlock()
	if len(records) != 2 {
		t.Fatalf("records=%#v", records)
	}
	if records[0].Path != "/v1/responses" || records[0].Action != "edit" || records[0].InputCount != 1 || !records[0].HasGeneratedParent || records[0].ContainsForbidden {
		t.Fatalf("responses image record=%#v", records[0])
	}
	if records[1].Path != "/v1/images/edits" || records[1].Action != "edit" || records[1].InputCount != 1 || !records[1].MaskPresent || records[1].ContainsForbidden {
		t.Fatalf("Images API record=%#v", records[1])
	}

	var decoded struct {
		Data []struct {
			B64JSON string `json:"b64_json"`
		} `json:"data"`
	}
	if json.Unmarshal(response.Body.Bytes(), &decoded) != nil || len(decoded.Data) != 1 || decoded.Data[0].B64JSON == "" {
		t.Fatalf("invalid deterministic image response")
	}
}
