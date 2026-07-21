package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"cargoflows/api/internal/ai"
	"cargoflows/api/internal/models"
)

var errImageEvalDisabled = errors.New("gpt-image-2 prompt evaluation is disabled; set OPENAI_IMAGE_PROMPT_EVAL=1 explicitly")

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	if err := run(ctx, os.Getenv, nil); err != nil {
		fmt.Fprintln(os.Stderr, "gpt-image-2 prompt evaluation failed:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, getenv func(string) string, client *http.Client) error {
	if getenv("OPENAI_IMAGE_PROMPT_EVAL") != "1" {
		return errImageEvalDisabled
	}
	key := []byte(strings.TrimSpace(getenv("OPENAI_API_KEY")))
	if len(key) == 0 {
		return errors.New("OPENAI_API_KEY is required")
	}
	defer clearBytes(key)
	baseURL := strings.TrimSpace(getenv("OPENAI_BASE_URL"))
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	if !officialBaseURL(baseURL) {
		return errors.New("OPENAI_BASE_URL must be the official https://api.openai.com/v1 endpoint")
	}
	target, targetMIME, err := readImage(getenv("OPENAI_IMAGE_TARGET_PATH"))
	if err != nil {
		return fmt.Errorf("target image: %w", err)
	}
	defer clearBytes(target)
	reference, referenceMIME, err := readImage(getenv("OPENAI_IMAGE_USAGE_REFERENCE_PATH"))
	if err != nil {
		return fmt.Errorf("usage reference image: %w", err)
	}
	defer clearBytes(reference)
	outputPath := strings.TrimSpace(getenv("OPENAI_IMAGE_OUTPUT_PATH"))
	if outputPath == "" {
		return errors.New("OPENAI_IMAGE_OUTPUT_PATH is required")
	}
	prompt, err := evaluationPrompt()
	if err != nil {
		return err
	}
	provider := ai.NewOpenAIImagesClient(baseURL, client, ai.OpenAIImagesConfig{Model: "gpt-image-2", MaxAttempts: 1, RequestTimeout: 210 * time.Second})
	response, err := provider.Generate(ctx, key, ai.ImageRequest{Model: "gpt-image-2", APIMode: "images", Prompt: prompt, Inputs: []ai.ImageInput{{MIMEType: targetMIME, Bytes: target}, {MIMEType: referenceMIME, Bytes: reference}}})
	if err != nil {
		return err
	}
	defer clearBytes(response.ImageBytes)
	if err := os.WriteFile(outputPath, response.ImageBytes, 0o600); err != nil {
		return fmt.Errorf("write output: %w", err)
	}
	fmt.Printf("request_id=%s model=%s total_tokens=%d output=%s\n", response.RequestID, response.Model, response.Usage.TotalTokens, outputPath)
	return nil
}

func evaluationPrompt() (ai.CompiledImagePrompt, error) {
	slot := ai.SlotFacts{
		PublicID: "20000000-0000-4000-8000-000000000004", SlotKey: "hero", Kind: models.AIContentSlotImage,
		Name: ai.LocalizedNameFacts{ZH: "主图", EN: "Hero image"}, Description: ai.LocalizedNameFacts{ZH: "手机壳装机展示", EN: "Fitted phone-case presentation"},
		PromptFragment: "Create a faithful ecommerce hero image of {{sku.code}}.", Constraints: json.RawMessage(`{"preserve_identity":true,"no_text":true}`),
		GenerationConfig: json.RawMessage(`{"candidate_count":1,"size":"1024x1024","quality":"medium","style":"soft_studio","allowed_styles":["soft_studio"]}`), LayoutConfig: json.RawMessage(`{"product_dominant":true}`),
	}
	referenceVersionID := "20000000-0000-4000-8000-000000000009"
	snapshot := ai.ProductSnapshotV1{
		Schema: ai.ProductSnapshotSchemaV1, Locale: "zh-CN", TargetPlatform: "prompt-evaluation",
		Product:             ai.ProductFacts{Name: "目标手机壳", Brand: "测试目标品牌", Description: "以目标图为唯一外观依据", Category: ai.CategoryFacts{NameZH: "手机壳", NameEN: "Phone cases"}},
		SKU:                 ai.SKUFacts{PublicID: "20000000-0000-4000-8000-000000000001", Code: "EVAL-TARGET-CASE", Color: "严格按目标图", CompatibleDeviceModel: "严格按目标图"},
		SOP:                 ai.SOPFacts{PublicID: "20000000-0000-4000-8000-000000000002", VersionPublicID: "20000000-0000-4000-8000-000000000003", VersionNumber: 1, SchemaVersion: "1.0", Name: ai.LocalizedNameFacts{ZH: "提示词回归", EN: "Prompt regression"}, Description: ai.LocalizedNameFacts{ZH: "验证主体身份约束", EN: "Validate subject identity constraints"}, CoordinateSystem: "pcs_object_v1"},
		Template:            ai.TemplateFacts{TemplatePublicID: "20000000-0000-4000-8000-000000000005", VersionPublicID: "20000000-0000-4000-8000-000000000006", VersionNumber: 1, PlatformPrompt: "Create a clean commercial product image without text.", SelectedSlots: []ai.SlotFacts{slot}},
		SelectedAssets:      []ai.AssetFacts{{PublicID: "20000000-0000-4000-8000-000000000007", SourceType: ai.AssetSourceProductVisual}},
		ReferenceSOPs:       []ai.ReferenceSOPFacts{{PublicID: "20000000-0000-4000-8000-000000000008", VersionPublicID: referenceVersionID, VersionNumber: 1, Name: ai.LocalizedNameFacts{ZH: "另一款手机壳使用效果", EN: "Another case usage effect"}, Description: ai.LocalizedNameFacts{ZH: "只参考装机关系和构图", EN: "Fitted relationship and composition only"}}},
		ExternalReferences:  []ai.ExternalReferenceFacts{{PublicID: "20000000-0000-4000-8000-000000000010", VersionPublicID: referenceVersionID, Purpose: models.AIReferenceUsageEffect, Caption: ai.LocalizedNameFacts{ZH: "另一款手机壳装机图", EN: "Another phone case fitted image"}, AllowedGuidance: ai.LocalizedNameFacts{ZH: "仅参考装机比例、空间关系和构图", EN: "Installed proportion, spatial relationship, and composition only"}, ForbiddenGuidance: ai.LocalizedNameFacts{ZH: "禁止继承外形、颜色、开孔、品牌、设备、配件、包装和文字", EN: "Do not inherit shape, color, cutouts, brand, device, accessories, packaging, or text"}}},
		GenerationOverrides: map[string]ai.GenerationOverride{},
	}
	return ai.CompileImagePrompt(snapshot, slot, ai.ImageTurnInput{Operation: models.AIExecutionGenerate, ThreadPublicID: "prompt-evaluation"})
}

func readImage(path string) ([]byte, string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, "", errors.New("path is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	extension := strings.ToLower(filepath.Ext(path))
	mimeType := map[string]string{".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".webp": "image/webp"}[extension]
	if mimeType == "" || len(data) == 0 {
		clearBytes(data)
		return nil, "", errors.New("image must be a non-empty PNG, JPEG, or WebP")
	}
	return data, mimeType, nil
}

func officialBaseURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimRight(strings.TrimSpace(raw), "/"))
	return err == nil && parsed.Scheme == "https" && parsed.Host == "api.openai.com" && parsed.Path == "/v1" && parsed.RawQuery == "" && parsed.Fragment == "" && parsed.User == nil
}

func clearBytes(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
