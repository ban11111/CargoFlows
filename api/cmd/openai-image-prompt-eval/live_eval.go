package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"cargoflows/api/internal/ai"
	"cargoflows/api/internal/app"
	"cargoflows/api/internal/config"
	"cargoflows/api/internal/database"
	"cargoflows/api/internal/models"
	"cargoflows/api/internal/secrets"
	"gorm.io/gorm"
)

const (
	liveEvalComposite = "composite"
	liveEvalBenefits  = "benefits"
	liveEvalStructure = "structure"
)

type liveEvaluationMetadata struct {
	Scenario          string        `json:"scenario"`
	Variant           string        `json:"variant"`
	TemplateVersion   string        `json:"template_version"`
	PromptCharacters  int           `json:"prompt_characters"`
	Duration          time.Duration `json:"duration"`
	RequestID         string        `json:"request_id"`
	Model             string        `json:"model"`
	InputTextTokens   int64         `json:"input_text_tokens"`
	InputImageTokens  int64         `json:"input_image_tokens"`
	OutputImageTokens int64         `json:"output_image_tokens"`
	TotalTokens       int64         `json:"total_tokens"`
	OutputPath        string        `json:"output_path"`
}

func runLiveEvaluation(ctx context.Context, getenv func(string) string, client *http.Client) error {
	jobPublicID := strings.TrimSpace(getenv("OPENAI_IMAGE_PROMPT_EVAL_JOB_ID"))
	versionPublicID := strings.TrimSpace(getenv("OPENAI_IMAGE_PROMPT_EVAL_VERSION_ID"))
	scenario := strings.TrimSpace(getenv("OPENAI_IMAGE_PROMPT_EVAL_SCENARIO"))
	variant := strings.TrimSpace(getenv("OPENAI_IMAGE_PROMPT_EVAL_VARIANT"))
	outputPath := strings.TrimSpace(getenv("OPENAI_IMAGE_OUTPUT_PATH"))
	if jobPublicID == "" || versionPublicID == "" || variant == "" || outputPath == "" {
		return errors.New("live evaluation requires job, template version, variant, and output path")
	}
	if scenario != liveEvalComposite && scenario != liveEvalBenefits && scenario != liveEvalStructure {
		return errors.New("OPENAI_IMAGE_PROMPT_EVAL_SCENARIO must be composite, benefits, or structure")
	}

	cfg := config.Load()
	if !officialBaseURL(cfg.OpenAIBaseURL) {
		return errors.New("OPENAI_BASE_URL must be the official https://api.openai.com/v1 endpoint")
	}
	db, err := database.Open(cfg.DatabaseDSN)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	prompt, inputs, err := loadLiveEvaluation(ctx, db, cfg, jobPublicID, versionPublicID, scenario)
	if err != nil {
		return err
	}
	if promptPath := strings.TrimSpace(getenv("OPENAI_IMAGE_PROMPT_EVAL_DUMP_PROMPT_PATH")); promptPath != "" {
		encoded, marshalErr := json.MarshalIndent(prompt, "", "  ")
		if marshalErr != nil {
			return fmt.Errorf("encode evaluation prompt: %w", marshalErr)
		}
		if err := os.WriteFile(promptPath, encoded, 0o600); err != nil {
			return fmt.Errorf("dump evaluation prompt: %w", err)
		}
	}
	for index := range inputs {
		defer clearBytes(inputs[index].Bytes)
	}
	if dumpDir := strings.TrimSpace(getenv("OPENAI_IMAGE_PROMPT_EVAL_DUMP_INPUT_DIR")); dumpDir != "" {
		if err := dumpEvaluationInputs(dumpDir, inputs); err != nil {
			return err
		}
	}
	if strings.TrimSpace(getenv("OPENAI_IMAGE_PROMPT_EVAL_COMPILE_ONLY")) == "1" {
		encoded, _ := json.Marshal(map[string]any{"scenario": scenario, "variant": variant, "template_version": versionPublicID, "prompt_characters": utf8.RuneCountInString(prompt.ImagesAPIPrompt()), "input_count": len(inputs), "size": prompt.ToolConfig.Size, "quality": prompt.ToolConfig.Quality, "sha256": prompt.SHA256})
		fmt.Println(string(encoded))
		return nil
	}
	credential, err := activeCredential(ctx, db, cfg.SecretsMasterKey)
	if err != nil {
		return err
	}
	defer clearBytes(credential.APIKey)
	model := strings.TrimSpace(credential.ImageGenerationModel)
	if model == "" {
		model = ai.DefaultOpenAIImageGenerationModel
	}
	provider := ai.NewOpenAIImagesClient(cfg.OpenAIBaseURL, client, ai.OpenAIImagesConfig{Model: model, MaxAttempts: 1, RequestTimeout: time.Duration(credential.ImageRequestTimeoutSeconds) * time.Second})
	started := time.Now()
	response, err := provider.Generate(ctx, credential.APIKey, ai.ImageRequest{Model: model, APIMode: "images", Prompt: prompt, Inputs: inputs})
	if err != nil {
		return err
	}
	defer clearBytes(response.ImageBytes)
	if err := os.WriteFile(outputPath, response.ImageBytes, 0o600); err != nil {
		return fmt.Errorf("write output: %w", err)
	}
	metadata := liveEvaluationMetadata{Scenario: scenario, Variant: variant, TemplateVersion: versionPublicID, PromptCharacters: utf8.RuneCountInString(prompt.ImagesAPIPrompt()), Duration: time.Since(started), RequestID: response.RequestID, Model: response.Model, InputTextTokens: response.Usage.InputTextTokens, InputImageTokens: response.Usage.InputImageTokens, OutputImageTokens: response.Usage.OutputImageTokens, TotalTokens: response.Usage.TotalTokens, OutputPath: outputPath}
	encoded, _ := json.Marshal(metadata)
	fmt.Println(string(encoded))
	return nil
}

func dumpEvaluationInputs(dir string, inputs []ai.ImageInput) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create evaluation input directory: %w", err)
	}
	for index, input := range inputs {
		extension := map[string]string{"image/png": ".png", "image/jpeg": ".jpg", "image/webp": ".webp"}[input.MIMEType]
		if extension == "" {
			return fmt.Errorf("dump evaluation input %d: unsupported MIME type %q", index+1, input.MIMEType)
		}
		path := filepath.Join(dir, fmt.Sprintf("input-%02d%s", index+1, extension))
		if err := os.WriteFile(path, input.Bytes, 0o600); err != nil {
			return fmt.Errorf("dump evaluation input %d: %w", index+1, err)
		}
	}
	return nil
}

func activeCredential(ctx context.Context, db *gorm.DB, encodedMasterKey string) (ai.ActiveOpenAICredential, error) {
	masterKey, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encodedMasterKey))
	if err != nil || len(masterKey) != 32 {
		clearBytes(masterKey)
		return ai.ActiveOpenAICredential{}, errors.New("CARGOFLOWS_SECRETS_MASTER_KEY must decode to 32 bytes")
	}
	box, err := secrets.NewAESGCM(masterKey)
	clearBytes(masterKey)
	if err != nil {
		return ai.ActiveOpenAICredential{}, err
	}
	credential, err := ai.NewProviderSettingsService(db, box, nil).DecryptActiveCredential(ctx)
	if err != nil {
		return ai.ActiveOpenAICredential{}, fmt.Errorf("decrypt active OpenAI credential: %w", err)
	}
	return credential, nil
}

func loadLiveEvaluation(ctx context.Context, db *gorm.DB, cfg config.Config, jobPublicID, versionPublicID, scenario string) (ai.CompiledImagePrompt, []ai.ImageInput, error) {
	var job models.AIJob
	if err := db.WithContext(ctx).Where("public_id = ?", jobPublicID).First(&job).Error; err != nil {
		return ai.CompiledImagePrompt{}, nil, fmt.Errorf("load evaluation job: %w", err)
	}
	var item models.AIJobItem
	if err := db.WithContext(ctx).Where("ai_job_id = ? AND kind = ?", job.ID, models.AIContentSlotImage).Order("id ASC").First(&item).Error; err != nil {
		return ai.CompiledImagePrompt{}, nil, fmt.Errorf("load evaluation image item: %w", err)
	}
	var snapshot ai.ProductSnapshotV1
	var frozenCanvas ai.SlotFacts
	if json.Unmarshal(job.InputSnapshotJSON, &snapshot) != nil || json.Unmarshal(item.SlotSnapshotJSON, &frozenCanvas) != nil {
		return ai.CompiledImagePrompt{}, nil, errors.New("evaluation job snapshot is invalid")
	}
	var version models.AIContentTemplateVersion
	if err := db.WithContext(ctx).Preload("Slots", func(tx *gorm.DB) *gorm.DB { return tx.Order("sequence ASC") }).Where("public_id = ?", versionPublicID).First(&version).Error; err != nil {
		return ai.CompiledImagePrompt{}, nil, fmt.Errorf("load evaluation template version: %w", err)
	}
	var sourceVersion models.AIContentTemplateVersion
	if err := db.WithContext(ctx).First(&sourceVersion, job.AIContentTemplateVersionID).Error; err != nil || sourceVersion.AIContentTemplateID != version.AIContentTemplateID {
		return ai.CompiledImagePrompt{}, nil, errors.New("evaluation template version must belong to the job's template")
	}
	var template models.AIContentTemplate
	if err := db.WithContext(ctx).First(&template, version.AIContentTemplateID).Error; err != nil {
		return ai.CompiledImagePrompt{}, nil, fmt.Errorf("load evaluation template: %w", err)
	}
	byKey := make(map[string]ai.SlotFacts, len(version.Slots))
	for _, stored := range version.Slots {
		byKey[stored.SlotKey] = evaluationSlotFacts(stored)
	}
	selected, err := evaluationScenarioSlot(byKey, frozenCanvas, scenario)
	if err != nil {
		return ai.CompiledImagePrompt{}, nil, err
	}
	snapshot.Template = ai.TemplateFacts{TemplatePublicID: template.PublicID, VersionPublicID: version.PublicID, VersionNumber: version.VersionNumber, PromptCompilerVersion: version.PromptCompilerVersion, PlatformPrompt: version.PlatformPrompt, SelectedSlots: append([]ai.SlotFacts{selected}, selected.CompositeRequirements...)}
	prompt, err := ai.CompileImagePrompt(snapshot, selected, ai.ImageTurnInput{Operation: models.AIExecutionGenerate, ThreadPublicID: "live-prompt-evaluation"})
	if err != nil {
		return ai.CompiledImagePrompt{}, nil, fmt.Errorf("compile live evaluation prompt: %w", err)
	}
	inputs, err := loadEvaluationInputs(ctx, db, cfg, job, item, snapshot, selected)
	if err != nil {
		return ai.CompiledImagePrompt{}, nil, err
	}
	return prompt, inputs, nil
}

func evaluationSlotFacts(slot models.AIContentSlot) ai.SlotFacts {
	return ai.SlotFacts{PublicID: slot.PublicID, SlotKey: slot.SlotKey, Kind: slot.Kind, Name: ai.LocalizedNameFacts{ZH: slot.NameZH, EN: slot.NameEN}, Description: ai.LocalizedNameFacts{ZH: slot.DescriptionZH, EN: slot.DescriptionEN}, Sequence: slot.Sequence, Optional: slot.Optional, DefaultSelected: slot.DefaultSelected, PromptFragment: slot.PromptFragment, Constraints: append(json.RawMessage(nil), slot.ConstraintsJSON...), GenerationConfig: append(json.RawMessage(nil), slot.GenerationConfigJSON...), LayoutConfig: append(json.RawMessage(nil), slot.LayoutConfigJSON...)}
}

func evaluationScenarioSlot(slots map[string]ai.SlotFacts, frozenCanvas ai.SlotFacts, scenario string) (ai.SlotFacts, error) {
	if scenario == liveEvalBenefits {
		return requiredEvaluationSlot(slots, "lazada_benefits_overview")
	}
	if scenario == liveEvalStructure {
		return requiredEvaluationSlot(slots, "lazada_structure_details")
	}
	keys := []string{"lazada_main_gallery", "lazada_case_on_device_studio", "lazada_case_on_device_handheld", "lazada_case_on_device_lifestyle"}
	requirements := make([]ai.SlotFacts, 0, len(keys))
	for _, key := range keys {
		slot, err := requiredEvaluationSlot(slots, key)
		if err != nil {
			return ai.SlotFacts{}, err
		}
		requirements = append(requirements, slot)
	}
	primary := requirements[0]
	primary.CanvasKey = frozenCanvas.CanvasKey
	primary.CanvasGeneration = frozenCanvas.CanvasGeneration
	primary.Name = frozenCanvas.Name
	primary.Description = frozenCanvas.Description
	primary.CompositeRequirements = requirements
	return primary, nil
}

func requiredEvaluationSlot(slots map[string]ai.SlotFacts, key string) (ai.SlotFacts, error) {
	slot, ok := slots[key]
	if !ok {
		return ai.SlotFacts{}, fmt.Errorf("template version is missing image slot %s", key)
	}
	return slot, nil
}

func loadEvaluationInputs(ctx context.Context, db *gorm.DB, cfg config.Config, job models.AIJob, item models.AIJobItem, snapshot ai.ProductSnapshotV1, slot ai.SlotFacts) ([]ai.ImageInput, error) {
	var assetIDs []string
	if json.Unmarshal(item.SelectedInputAssetIDsJSON, &assetIDs) != nil || len(assetIDs) == 0 {
		return nil, errors.New("evaluation image item has no selected assets")
	}
	var assets []models.Asset
	if err := db.WithContext(ctx).Where("public_id IN ? AND sk_uid = ? AND review_status = ?", assetIDs, job.SKUID, "approved").Find(&assets).Error; err != nil || len(assets) != len(assetIDs) {
		return nil, errors.New("evaluation source assets are unavailable")
	}
	byID := make(map[string]models.Asset, len(assets))
	for _, asset := range assets {
		byID[asset.PublicID] = asset
	}
	productAssets := ai.ImageGenerationProductAssets(snapshot, slot)
	keys := make([]string, 0, len(productAssets)+len(snapshot.BrandIcons)+len(snapshot.StructureReferences)+len(snapshot.StyleReferences))
	for _, fact := range productAssets {
		stored, ok := byID[fact.PublicID]
		if !ok {
			return nil, errors.New("evaluation source asset order is inconsistent")
		}
		keys = append(keys, stored.ObjectKey)
	}
	for _, reference := range snapshot.BrandIcons {
		var stored models.BrandIcon
		if err := db.WithContext(ctx).Where("public_id = ? AND sha256 = ?", reference.PublicID, reference.SHA256).First(&stored).Error; err != nil {
			return nil, errors.New("evaluation brand icon is unavailable")
		}
		keys = append(keys, stored.ObjectKey)
	}
	for _, reference := range snapshot.StructureReferences {
		var stored models.ModelFamilyReferenceAsset
		if err := db.WithContext(ctx).Where("public_id = ? AND derivative_sha256 = ?", reference.PublicID, reference.DerivativeSHA256).First(&stored).Error; err != nil {
			return nil, errors.New("evaluation structure reference is unavailable")
		}
		keys = append(keys, stored.DerivativeObjectKey)
	}
	for _, reference := range snapshot.StyleReferences {
		var stored models.StyleReferenceGrant
		if err := db.WithContext(ctx).Where("public_id = ? AND derivative_sha256 = ?", reference.PublicID, reference.DerivativeSHA256).First(&stored).Error; err != nil {
			return nil, errors.New("evaluation style reference is unavailable")
		}
		keys = append(keys, stored.DerivativeObjectKey)
	}
	// Production deliberately keeps raw external SOP images out of gpt-image-2
	// binary inputs. The evaluator must mirror that identity-firewall policy.
	objects, err := app.NewImageObjectStore(cfg)
	if err != nil {
		return nil, err
	}
	storage := ai.NewImageStorage(objects)
	inputs := make([]ai.ImageInput, 0, len(keys))
	for _, key := range keys {
		input, err := storage.ReadSource(ctx, key)
		if err != nil {
			for index := range inputs {
				clearBytes(inputs[index].Bytes)
			}
			return nil, fmt.Errorf("read evaluation input: %w", err)
		}
		inputs = append(inputs, input)
	}
	return inputs, nil
}
