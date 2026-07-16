package app

import (
	"os"
	"testing"

	"github.com/goccy/go-yaml"
)

func TestCaptureSOPOpenAPIHasCompleteRuntimeErrorResponses(t *testing.T) {
	contents, err := os.ReadFile("../../openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := yaml.Unmarshal(contents, &document); err != nil {
		t.Fatal(err)
	}
	paths := document["paths"].(map[string]any)
	expected := map[string]map[string][]string{
		"/capture-sops":                                                          {"post": {"201", "400", "401", "403", "404", "500"}, "get": {"200", "400", "401", "403", "500"}},
		"/capture-sops/{sop_id}":                                                 {"get": {"200", "400", "401", "404", "500"}},
		"/capture-sops/{sop_id}/versions":                                        {"post": {"201", "400", "401", "403", "404", "409", "500"}},
		"/sop-versions/{version_id}":                                             {"get": {"200", "400", "401", "404", "500"}, "patch": {"200", "400", "401", "403", "404", "409", "428", "500"}},
		"/sop-versions/{version_id}/views":                                       {"post": {"201", "400", "401", "403", "404", "409", "428", "500"}},
		"/sop-versions/{version_id}/views/{view_id}":                             {"patch": {"200", "400", "401", "403", "404", "409", "428", "500"}, "delete": {"200", "400", "401", "403", "404", "409", "428", "500"}},
		"/sop-versions/{version_id}/view-order":                                  {"put": {"200", "400", "401", "403", "404", "409", "428", "500"}},
		"/sop-versions/{version_id}/validate":                                    {"post": {"200", "400", "401", "403", "404", "500"}},
		"/sop-versions/{version_id}/publish":                                     {"post": {"200", "400", "401", "403", "404", "409", "422", "428", "500"}},
		"/sop-versions/{version_id}/archive":                                     {"post": {"200", "400", "401", "403", "404", "409", "500"}},
		"/sop-versions/{version_id}/views/{view_id}/reference-images/upload-url": {"post": {"200", "400", "401", "403", "404", "409", "500", "503"}},
		"/sop-versions/{version_id}/views/{view_id}/reference-images":            {"post": {"201", "400", "401", "403", "404", "409", "428", "500"}},
		"/sop-versions/{version_id}/views/{view_id}/reference-images/{image_id}": {"delete": {"200", "400", "401", "403", "404", "409", "428", "500"}},
		"/sop-versions/{version_id}/views/{view_id}/reference-image-order":       {"put": {"200", "400", "401", "403", "404", "409", "428", "500"}},
		"/photo-sessions":                                                        {"post": {"201", "400", "401", "404", "409", "500"}},
		"/assets/upload-url":                                                     {"post": {"200", "400", "401", "403", "404", "409", "500", "503"}},
		"/assets/complete":                                                       {"post": {"200", "201", "400", "401", "403", "404", "409", "500", "503"}},
		"/assets/review":                                                         {"get": {"200", "401", "500"}},
		"/assets/review/hierarchy":                                               {"get": {"200", "401", "500"}},
	}
	for path, methods := range expected {
		pathItem, ok := paths[path].(map[string]any)
		if !ok {
			t.Errorf("missing path %s", path)
			continue
		}
		for method, statuses := range methods {
			operation, ok := pathItem[method].(map[string]any)
			if !ok {
				t.Errorf("missing operation %s %s", method, path)
				continue
			}
			responses, ok := operation["responses"].(map[string]any)
			if !ok {
				t.Errorf("missing responses for %s %s", method, path)
				continue
			}
			for _, status := range statuses {
				if _, ok := responses[status]; !ok {
					t.Errorf("%s %s missing response %s", method, path, status)
				}
			}
		}
	}
	components := document["components"].(map[string]any)
	responses := components["responses"].(map[string]any)
	for _, name := range []string{"BadRequest", "Unauthorized", "Forbidden", "NotFound", "Conflict", "PreconditionRequired", "ValidationFailed", "InternalServerError", "ServiceUnavailable"} {
		if _, ok := responses[name]; !ok {
			t.Errorf("missing reusable response %s", name)
		}
	}
}

func TestOpenAPIUsesDistinctUploadResponseSchemas(t *testing.T) {
	contents, err := os.ReadFile("../../openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := yaml.Unmarshal(contents, &document); err != nil {
		t.Fatal(err)
	}
	paths := document["paths"].(map[string]any)
	schemaRef := func(path string) string {
		operation := paths[path].(map[string]any)["post"].(map[string]any)
		responses := operation["responses"].(map[string]any)
		response := responses["200"].(map[string]any)
		content := response["content"].(map[string]any)
		mediaType := content["application/json"].(map[string]any)
		return mediaType["schema"].(map[string]any)["$ref"].(string)
	}
	if got := schemaRef("/assets/upload-url"); got != "#/components/schemas/AssetUploadEnvelope" {
		t.Fatalf("asset upload response must require its completion ticket, got %q", got)
	}
	if got := schemaRef("/sop-versions/{version_id}/views/{view_id}/reference-images/upload-url"); got != "#/components/schemas/SOPReferenceUploadEnvelope" {
		t.Fatalf("SOP reference upload response must use its runtime schema without a completion ticket, got %q", got)
	}

	schemas := document["components"].(map[string]any)["schemas"].(map[string]any)
	requiredFields := func(name string) map[string]bool {
		result := map[string]bool{}
		for _, value := range schemas[name].(map[string]any)["required"].([]any) {
			result[value.(string)] = true
		}
		return result
	}
	if !requiredFields("AssetUploadEnvelope")["completion_token"] {
		t.Fatal("asset upload response must require completion_token")
	}
	if requiredFields("SOPReferenceUploadEnvelope")["completion_token"] {
		t.Fatal("SOP reference upload response must not claim a completion_token")
	}
}
