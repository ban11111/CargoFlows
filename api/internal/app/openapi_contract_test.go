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
		"/sop-reference-images/{image_id}/media":                                 {"get": {"200", "400", "401", "404", "503"}},
		"/assets/{asset_id}/media":                                               {"get": {"200", "400", "401", "403", "404", "503"}},
		"/assets/{asset_id}/review":                                              {"patch": {"200", "400", "401", "403", "404", "500"}},
		"/assets/review":                                                         {"get": {"200", "401", "403", "500"}},
		"/assets/review/hierarchy":                                               {"get": {"200", "401", "403", "500"}},
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
	assetUploadProperties := schemas["AssetUploadEnvelope"].(map[string]any)["properties"].(map[string]any)
	for _, forbidden := range []string{"asset_url", "object_key"} {
		if _, exists := assetUploadProperties[forbidden]; exists {
			t.Errorf("asset upload response exposes internal locator %s", forbidden)
		}
	}
	if requiredFields("SOPReferenceUploadEnvelope")["completion_token"] {
		t.Fatal("SOP reference upload response must not claim a completion_token")
	}
}

func TestOpenAPIPublicIdentityAndAssetSafetyContract(t *testing.T) {
	contents, err := os.ReadFile("../../openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := yaml.Unmarshal(contents, &document); err != nil {
		t.Fatal(err)
	}
	paths := document["paths"].(map[string]any)
	for _, path := range []string{"/skus/{sku_id}", "/skus/{sku_id}/inventory-adjustments", "/skus/{sku_id}/platform-content", "/assets/{asset_id}/media", "/assets/{asset_id}/review"} {
		if _, exists := paths[path]; !exists {
			t.Errorf("missing public UUID path %s", path)
		}
	}
	if _, exists := paths["/skus/{id}/platform-content"]; exists {
		t.Error("legacy numeric SKU platform-content path remains public")
	}

	schemas := document["components"].(map[string]any)["schemas"].(map[string]any)
	assertUUIDProperty := func(schemaName, propertyName string) {
		t.Helper()
		property := schemas[schemaName].(map[string]any)["properties"].(map[string]any)[propertyName].(map[string]any)
		if property["type"] != "string" || property["format"] != "uuid" {
			t.Errorf("%s.%s is not a UUID: %#v", schemaName, propertyName, property)
		}
	}
	for _, pair := range [][2]string{{"SKU", "public_id"}, {"CreatePhotoSessionRequest", "sku_id"}, {"PhotoSession", "sku_id"}, {"CreateAIJobRequest", "sku_id"}, {"AIJob", "sku_id"}, {"PlatformContent", "sku_id"}, {"AISnapshotSKU", "public_id"}, {"AISnapshotAsset", "public_id"}, {"CompletedAsset", "public_id"}, {"CompletedAsset", "sku_id"}, {"AssetReviewItem", "public_id"}, {"AssetReviewItem", "sku_id"}, {"AssetReviewSKU", "public_id"}, {"AssetReviewHierarchyAsset", "public_id"}} {
		assertUUIDProperty(pair[0], pair[1])
	}
	for _, schemaName := range []string{"AISnapshotAsset", "CompletedAsset", "AssetReviewItem", "AssetReviewHierarchyAsset"} {
		properties := schemas[schemaName].(map[string]any)["properties"].(map[string]any)
		for _, forbidden := range []string{"id", "object_key", "original_url", "thumbnail_url"} {
			if _, exists := properties[forbidden]; exists {
				t.Errorf("%s exposes internal field %s", schemaName, forbidden)
			}
		}
	}
}
