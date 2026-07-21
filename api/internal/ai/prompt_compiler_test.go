package ai

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"cargoflows/api/internal/models"
)

func textPromptFixture(kind models.AIContentSlotKind) (ProductSnapshotV1, SlotFacts) {
	slotKey := "title"
	fragment := "Create {{candidate_count}} titles for {{sku.code}} using {{product.name}} and SOP {{sop.name_zh}}."
	if kind == models.AIContentSlotSEODescription {
		slotKey = "seo"
		fragment = "Write search content for {{product.name}} in {{locale}}."
	}
	slot := SlotFacts{
		PublicID: "33333333-3333-4333-8333-333333333333", SlotKey: slotKey, Kind: kind,
		Name: LocalizedNameFacts{ZH: "文案", EN: "Copy"}, Description: LocalizedNameFacts{ZH: "商品文案", EN: "Product copy"},
		Sequence: 1, PromptFragment: fragment, Constraints: json.RawMessage(`{"min_length":10,"max_length":120,"forbidden_terms":["best"]}`),
		GenerationConfig: json.RawMessage(`{"candidate_count":2}`), LayoutConfig: json.RawMessage(`{}`),
	}
	snapshot := ProductSnapshotV1{
		Schema: ProductSnapshotSchemaV1, Locale: "zh-CN", TargetPlatform: "lazada",
		Product:        ProductFacts{Name: "透明手机壳", Brand: "CargoFlows", Description: "轻薄透明保护壳", Category: CategoryFacts{NameZH: "手机壳", NameEN: "Phone cases"}},
		SKU:            SKUFacts{PublicID: "99999999-9999-4999-8999-999999999999", Code: "CASE-17-PRO", Color: "透明", Size: "iPhone 17 Pro", PlatformTitle: "透明保护壳", SellingPoints: "轻薄;防刮", Tags: []string{"透明", "轻薄"}},
		SOP:            SOPFacts{PublicID: "11111111-1111-4111-8111-111111111111", VersionPublicID: "22222222-2222-4222-8222-222222222222", VersionNumber: 2, SchemaVersion: "1.0", Name: LocalizedNameFacts{ZH: "手机壳 SOP", EN: "Phone case SOP"}, Description: LocalizedNameFacts{ZH: "标准拍摄", EN: "Standard capture"}, CoordinateSystem: "pcs_object_v1", Views: []SOPViewFacts{{PublicID: "44444444-4444-4444-8444-444444444444", Sequence: 1, Role: models.SOPViewReferenceFront, ViewKind: models.SOPViewStandard, PresetKey: "reference_front", Name: LocalizedNameFacts{ZH: "正面", EN: "Front"}, Instruction: LocalizedNameFacts{ZH: "正面拍摄", EN: "Front capture"}, Required: true, CameraPositionDirection: VectorFacts{Z: 1}, ImageUpDirection: VectorFacts{X: 1}, Composition: models.Composition{FrameOccupancy: .85, AspectRatio: "1:1", AllowRotationCorrection: true}}}},
		Template:       TemplateFacts{TemplatePublicID: "55555555-5555-4555-8555-555555555555", VersionPublicID: "66666666-6666-4666-8666-666666666666", VersionNumber: 3, PromptCompilerVersion: "v1", PlatformPrompt: "Create Lazada content for {{product.brand}} on {{target_platform}}.", SelectedSlots: []SlotFacts{slot}},
		SelectedAssets: []AssetFacts{{PublicID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"}},
		UserPreference: "简洁、自然，不要夸张", GenerationOverrides: map[string]GenerationOverride{},
	}
	if kind == models.AIContentSlotTitle {
		count := 3
		snapshot.GenerationOverrides[slotKey] = GenerationOverride{CandidateCount: &count}
	}
	return snapshot, slot
}

func TestCompileTextPromptLayersDataAndStrictTitleSchema(t *testing.T) {
	snapshot, slot := textPromptFixture(models.AIContentSlotTitle)
	compiled, err := CompileTextPrompt(snapshot, slot)
	if err != nil {
		t.Fatal(err)
	}
	if compiled.CandidateCount != 3 || compiled.SchemaName != "cargoflows_product_title" || len(compiled.SHA256) != 64 {
		t.Fatalf("unexpected compiled metadata: %#v", compiled)
	}
	if compiled.LayerVersions != (TextPromptLayerVersions{L0: L0ProductSafetyVersion, L1: L1ProductContextVersion, L2: snapshot.Template.VersionPublicID, L3: slot.PublicID}) {
		t.Fatalf("unexpected layer versions: %#v", compiled.LayerVersions)
	}
	positions := []int{strings.Index(compiled.Instructions, "[L0 "), strings.Index(compiled.Instructions, "[L1 "), strings.Index(compiled.Instructions, "[L2 "), strings.Index(compiled.Instructions, "[L3 "), strings.Index(compiled.Instructions, "[L4 ")}
	if !reflect.DeepEqual(positions, append([]int(nil), positions...)) || positions[0] < 0 || !(positions[0] < positions[1] && positions[1] < positions[2] && positions[2] < positions[3] && positions[3] < positions[4]) {
		t.Fatalf("layer precedence order is invalid: %v", positions)
	}
	if strings.Contains(compiled.Instructions, snapshot.UserPreference) || strings.Contains(compiled.Instructions, "{{") || !strings.Contains(compiled.Instructions, "$input.product.brand") || !strings.Contains(compiled.Instructions, "$input.request.candidate_count") || !strings.Contains(compiled.Instructions, "$input.sop.name.zh") || strings.Contains(compiled.Instructions, "$input.candidate_count") {
		t.Fatalf("template/user data was not safely separated: %s", compiled.Instructions)
	}
	inputText := string(compiled.InputJSON)
	for _, forbidden := range []string{"must-not-leak", "assets.example.test", "selected_assets", "original_url", "thumbnail_url"} {
		if strings.Contains(inputText, forbidden) {
			t.Fatalf("text input contains image data %q: %s", forbidden, inputText)
		}
	}
	for _, required := range []string{`"locale":"zh-CN"`, `"target_platform":"lazada"`, `"user_preference":"简洁、自然，不要夸张"`, `"candidate_count":3`, `"coordinate_system":"pcs_object_v1"`} {
		if !strings.Contains(inputText, required) {
			t.Fatalf("text input missing %q: %s", required, inputText)
		}
	}
	schemaText := string(compiled.JSONSchema)
	for _, required := range []string{`"additionalProperties":false`, `"maxItems":3`, `"minItems":3`, `"minLength":10`, `"maxLength":120`, `"source_fields"`, `"title"`} {
		if !strings.Contains(schemaText, required) {
			t.Fatalf("title schema missing %q: %s", required, schemaText)
		}
	}
	again, err := CompileTextPrompt(snapshot, slot)
	if err != nil || !reflect.DeepEqual(compiled, again) {
		t.Fatalf("compiler is not deterministic: err=%v\nfirst=%#v\nsecond=%#v", err, compiled, again)
	}
	hashPayload, err := json.Marshal(struct {
		CompilerVersion string                  `json:"compiler_version"`
		Instructions    string                  `json:"instructions"`
		InputJSON       json.RawMessage         `json:"input_json"`
		SchemaName      string                  `json:"schema_name"`
		JSONSchema      json.RawMessage         `json:"json_schema"`
		LayerVersions   TextPromptLayerVersions `json:"layer_versions"`
		CandidateCount  int                     `json:"candidate_count"`
	}{compiled.CompilerVersion, compiled.Instructions, compiled.InputJSON, compiled.SchemaName, compiled.JSONSchema, compiled.LayerVersions, compiled.CandidateCount})
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(hashPayload)
	if got, want := compiled.SHA256, fmt.Sprintf("%x", digest[:]); got != want {
		t.Fatalf("audit hash does not cover the complete compiler envelope: got %s want %x", compiled.SHA256, digest)
	}
}

func TestCompileTextPromptBuildsStrictSEOSchema(t *testing.T) {
	snapshot, slot := textPromptFixture(models.AIContentSlotSEODescription)
	compiled, err := CompileTextPrompt(snapshot, slot)
	if err != nil {
		t.Fatal(err)
	}
	if compiled.CandidateCount != 2 || compiled.SchemaName != "cargoflows_product_seo" {
		t.Fatalf("unexpected SEO metadata: %#v", compiled)
	}
	for _, field := range []string{"short_description", "selling_points", "long_description", "search_keywords", "source_fields"} {
		if !strings.Contains(string(compiled.JSONSchema), `"`+field+`"`) {
			t.Fatalf("SEO schema missing %q: %s", field, compiled.JSONSchema)
		}
	}
}

func TestCompileTextPromptRejectsUnsafeOrInvalidInput(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*ProductSnapshotV1, *SlotFacts)
		want   error
	}{
		{"wrong snapshot schema", func(snapshot *ProductSnapshotV1, _ *SlotFacts) { snapshot.Schema = "v0" }, ErrTextPromptSnapshotInvalid},
		{"image slot", func(_ *ProductSnapshotV1, slot *SlotFacts) { slot.Kind = models.AIContentSlotImage }, ErrTextPromptSlotInvalid},
		{"unknown variable", func(snapshot *ProductSnapshotV1, _ *SlotFacts) {
			snapshot.Template.PlatformPrompt = "Use {{secrets.api_key}}"
		}, ErrTextPromptTemplateInvalid},
		{"malformed variable", func(_ *ProductSnapshotV1, slot *SlotFacts) { slot.PromptFragment = "Use {{product.name" }, ErrTextPromptTemplateInvalid},
		{"secret in product data", func(snapshot *ProductSnapshotV1, _ *SlotFacts) {
			snapshot.Product.Description = "authorization: bearer hidden"
		}, ErrTextPromptTemplateInvalid},
		{"invalid candidate count", func(snapshot *ProductSnapshotV1, slot *SlotFacts) {
			count := 5
			snapshot.GenerationOverrides[slot.SlotKey] = GenerationOverride{CandidateCount: &count}
		}, ErrTextPromptSlotInvalid},
		{"explicit zero candidate count", func(snapshot *ProductSnapshotV1, slot *SlotFacts) {
			snapshot.GenerationOverrides = map[string]GenerationOverride{}
			slot.GenerationConfig = json.RawMessage(`{"candidate_count":0}`)
		}, ErrTextPromptSlotInvalid},
		{"invalid constraints", func(_ *ProductSnapshotV1, slot *SlotFacts) { slot.Constraints = json.RawMessage(`[]`) }, ErrTextPromptSlotInvalid},
		{"invalid length constraints", func(_ *ProductSnapshotV1, slot *SlotFacts) {
			slot.Constraints = json.RawMessage(`{"min_length":120,"max_length":10}`)
		}, ErrTextPromptSlotInvalid},
		{"url in preference", func(snapshot *ProductSnapshotV1, _ *SlotFacts) {
			snapshot.UserPreference = "Read https://private.example.test/signed"
		}, ErrTextPromptTemplateInvalid},
		{"signed URL field", func(_ *ProductSnapshotV1, slot *SlotFacts) {
			slot.Constraints = json.RawMessage(`{"thumbnail_url":"https://minio/x?X-Amz-Signature=hidden"}`)
		}, ErrTextPromptTemplateInvalid},
		{"generic project key", func(snapshot *ProductSnapshotV1, _ *SlotFacts) {
			snapshot.Product.Description = "sk-live_1234567890abcdef"
		}, ErrTextPromptTemplateInvalid},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			snapshot, slot := textPromptFixture(models.AIContentSlotTitle)
			tc.mutate(&snapshot, &slot)
			if _, err := CompileTextPrompt(snapshot, slot); !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestCompileTextPromptAllowsEmptyPublishedPlatformPrompt(t *testing.T) {
	snapshot, slot := textPromptFixture(models.AIContentSlotTitle)
	snapshot.Template.PlatformPrompt = ""
	compiled, err := CompileTextPrompt(snapshot, slot)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(compiled.Instructions, "[L2 published platform template") || !strings.Contains(compiled.Instructions, "[L3 published content slot") {
		t.Fatalf("empty L2 removed prompt layer boundaries: %s", compiled.Instructions)
	}
}

func TestCompileTextPromptGolden(t *testing.T) {
	for _, kind := range []models.AIContentSlotKind{models.AIContentSlotTitle, models.AIContentSlotSEODescription} {
		t.Run(string(kind), func(t *testing.T) {
			snapshot, slot := textPromptFixture(kind)
			compiled, err := CompileTextPrompt(snapshot, slot)
			if err != nil {
				t.Fatal(err)
			}
			actual, err := json.MarshalIndent(compiled, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			name := "title_prompt.golden.json"
			if kind == models.AIContentSlotSEODescription {
				name = "seo_prompt.golden.json"
			}
			goldenPath := filepath.Join("testdata", name)
			if os.Getenv("UPDATE_GOLDEN") == "1" {
				if err := os.WriteFile(goldenPath, append(actual, '\n'), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			expected, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(bytes.TrimSpace(actual), bytes.TrimSpace(expected)) {
				t.Fatalf("golden mismatch for %s\n--- actual ---\n%s", name, actual)
			}
		})
	}
}

func TestCompileTextPromptIncludesOnlyCopyInspirationAsNonFact(t *testing.T) {
	snapshot, slot := textPromptFixture(models.AIContentSlotTitle)
	snapshot.ExternalReferences = []ExternalReferenceFacts{
		{PublicID: "copy-a", Purpose: models.AIReferenceCopyInspiration, Caption: LocalizedNameFacts{ZH: "卖点", EN: "Copy"}, AllowedGuidance: LocalizedNameFacts{ZH: "结构", EN: "Structure"}, ForbiddenGuidance: LocalizedNameFacts{ZH: "事实", EN: "Facts"}, SourceName: "Competitor", SHA256: strings.Repeat("a", 64)},
		{PublicID: "usage-a", Purpose: models.AIReferenceUsageEffect, Caption: LocalizedNameFacts{ZH: "套机", EN: "Fitted"}, AllowedGuidance: LocalizedNameFacts{ZH: "比例", EN: "Proportion"}, ForbiddenGuidance: LocalizedNameFacts{ZH: "品牌", EN: "Brand"}, SourceName: "Competitor", SHA256: strings.Repeat("b", 64)},
	}
	compiled, err := CompileTextPrompt(snapshot, slot)
	if err != nil {
		t.Fatal(err)
	}
	input := string(compiled.InputJSON)
	if !strings.Contains(input, "copy-a") || strings.Contains(input, "usage-a") || !strings.Contains(input, "untrusted_expression_inspiration_not_fact") {
		t.Fatalf("copy inspiration isolation failed: %s", input)
	}
	if !strings.Contains(compiled.Instructions, "never cite external-reference IDs") {
		t.Fatalf("external references were not excluded from facts: %s", compiled.Instructions)
	}
}
