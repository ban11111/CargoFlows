package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"cargoflow/api/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	defaultTemplateLocale        = "zh-CN"
	defaultPromptCompilerVersion = "v1"
	templateDraftGuard           = "draft"
)

var (
	ErrTemplateNotFound           = errors.New("AI content template not found")
	ErrTemplateVersionNotFound    = errors.New("AI content template version not found")
	ErrTemplateVersionImmutable   = errors.New("AI content template version is immutable")
	ErrTemplateDraftExists        = errors.New("an AI content template draft already exists")
	ErrTemplateSourceNotPublished = errors.New("source AI content template version is not published")
)

type SlotInput struct {
	SlotKey          string
	Kind             string
	NameZH           string
	NameEN           string
	DescriptionZH    string
	DescriptionEN    string
	Sequence         int
	Optional         bool
	DefaultSelected  bool
	PromptFragment   string
	Constraints      json.RawMessage
	GenerationConfig json.RawMessage
	LayoutConfig     json.RawMessage
}

type UpdateTemplateVersionInput struct {
	NameZH                string
	NameEN                string
	TargetPlatform        string
	DefaultLocale         string
	PromptCompilerVersion string
	PlatformPrompt        string
	Slots                 []SlotInput
}

type CreateTemplateInput struct {
	NameZH         string
	NameEN         string
	TargetPlatform string
	CreatedByID    uint
	Version        UpdateTemplateVersionInput
}

type CreatedTemplate struct {
	Template models.AIContentTemplate
	Version  models.AIContentTemplateVersion
}

type ValidationIssue struct {
	Code    string `json:"code"`
	Path    string `json:"path"`
	Message string `json:"message"`
}

type TemplateService struct {
	db *gorm.DB
}

func NewTemplateService(db *gorm.DB) *TemplateService { return &TemplateService{db: db} }

func (s *TemplateService) Create(ctx context.Context, input CreateTemplateInput) (*CreatedTemplate, error) {
	var created CreatedTemplate
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		created.Template = models.AIContentTemplate{
			PublicID: uuid.NewString(), NameZH: strings.TrimSpace(input.NameZH), NameEN: strings.TrimSpace(input.NameEN),
			TargetPlatform: strings.TrimSpace(input.TargetPlatform), Status: models.AIContentTemplateActive, CreatedByID: input.CreatedByID,
		}
		if err := tx.Create(&created.Template).Error; err != nil {
			return err
		}
		created.Version = newTemplateVersion(created.Template.ID, 1, input.CreatedByID, input.Version)
		if err := tx.Create(&created.Version).Error; err != nil {
			return err
		}
		slots := normalizeSlotInputs(created.Version.ID, input.Version.Slots)
		if len(slots) != 0 {
			if err := tx.Create(&slots).Error; err != nil {
				return err
			}
		}
		created.Version.Slots = slots
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &created, nil
}

func (s *TemplateService) Get(ctx context.Context, publicID string) (*models.AIContentTemplate, error) {
	var result models.AIContentTemplate
	err := s.db.WithContext(ctx).
		Preload("Versions", func(db *gorm.DB) *gorm.DB { return db.Order("version_number ASC") }).
		Preload("Versions.Slots", func(db *gorm.DB) *gorm.DB { return db.Order("sequence ASC, id ASC") }).
		Where("public_id = ?", publicID).First(&result).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrTemplateNotFound
	}
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (s *TemplateService) GetVersion(ctx context.Context, publicID string) (*models.AIContentTemplateVersion, error) {
	return getTemplateVersion(s.db.WithContext(ctx), publicID)
}

func (s *TemplateService) Validate(ctx context.Context, versionPublicID string) ([]ValidationIssue, error) {
	version, err := getTemplateVersion(s.db.WithContext(ctx), versionPublicID)
	if err != nil {
		return nil, err
	}
	var parent models.AIContentTemplate
	if err := s.db.WithContext(ctx).First(&parent, version.AIContentTemplateID).Error; err != nil {
		return nil, err
	}
	issues := validateTemplateNames(parent)
	issues = append(issues, ValidateTemplateVersion(*version, version.Slots)...)
	return issues, nil
}

func (s *TemplateService) List(ctx context.Context, includeAll bool) ([]models.AIContentTemplate, error) {
	var result []models.AIContentTemplate
	db := s.db.WithContext(ctx).Model(&models.AIContentTemplate{})
	if includeAll {
		db = db.Preload("Versions", func(db *gorm.DB) *gorm.DB { return db.Order("version_number ASC") })
	} else {
		db = db.
			Joins("JOIN ai_content_template_versions selectable_versions ON selectable_versions.ai_content_template_id = ai_content_templates.id AND selectable_versions.status = ?", models.AITemplatePublished).
			Distinct("ai_content_templates.*").
			Preload("Versions", "status = ?", models.AITemplatePublished)
	}
	err := db.
		Preload("Versions.Slots", func(db *gorm.DB) *gorm.DB { return db.Order("sequence ASC, id ASC") }).
		Order("ai_content_templates.id ASC").Find(&result).Error
	return result, err
}

func (s *TemplateService) UpdateDraft(ctx context.Context, versionPublicID string, input UpdateTemplateVersionInput) (*models.AIContentTemplateVersion, error) {
	var updated *models.AIContentTemplateVersion
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		version, err := getTemplateVersion(tx.Clauses(clause.Locking{Strength: "UPDATE"}), versionPublicID)
		if err != nil {
			return err
		}
		if version.Status != models.AITemplateDraft {
			return ErrTemplateVersionImmutable
		}
		var parent models.AIContentTemplate
		if err := tx.First(&parent, version.AIContentTemplateID).Error; err != nil {
			return err
		}
		parentUpdates := map[string]any{"name_zh": strings.TrimSpace(input.NameZH), "name_en": strings.TrimSpace(input.NameEN)}
		if strings.TrimSpace(input.TargetPlatform) != "" {
			parentUpdates["target_platform"] = strings.TrimSpace(input.TargetPlatform)
		}
		if err := tx.Model(&parent).Updates(parentUpdates).Error; err != nil {
			return err
		}
		if err := tx.Model(version).Updates(map[string]any{
			"default_locale":          normalizedDefault(input.DefaultLocale, defaultTemplateLocale),
			"prompt_compiler_version": normalizedDefault(input.PromptCompilerVersion, defaultPromptCompilerVersion),
			"platform_prompt":         strings.TrimSpace(input.PlatformPrompt),
		}).Error; err != nil {
			return err
		}
		if err := tx.Where("ai_content_template_version_id = ?", version.ID).Delete(&models.AIContentSlot{}).Error; err != nil {
			return err
		}
		slots := normalizeSlotInputs(version.ID, input.Slots)
		if len(slots) != 0 {
			if err := tx.Create(&slots).Error; err != nil {
				return err
			}
		}
		updated, err = getTemplateVersion(tx, versionPublicID)
		return err
	})
	return updated, err
}

// ReplaceDraft is retained as a narrow compatibility alias for callers using the
// original implementation-plan name.
func (s *TemplateService) ReplaceDraft(ctx context.Context, versionPublicID string, input UpdateTemplateVersionInput) error {
	_, err := s.UpdateDraft(ctx, versionPublicID, input)
	return err
}

func (s *TemplateService) Publish(ctx context.Context, versionPublicID string, actorID uint) ([]ValidationIssue, error) {
	var issues []ValidationIssue
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		version, err := getTemplateVersion(tx.Clauses(clause.Locking{Strength: "UPDATE"}), versionPublicID)
		if err != nil {
			return err
		}
		if version.Status != models.AITemplateDraft {
			return ErrTemplateVersionImmutable
		}
		var parent models.AIContentTemplate
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&parent, version.AIContentTemplateID).Error; err != nil {
			return err
		}
		issues = validateTemplateNames(parent)
		issues = append(issues, ValidateTemplateVersion(*version, version.Slots)...)
		if len(issues) != 0 {
			return nil
		}
		now := time.Now().UTC()
		if err := tx.Model(version).Updates(map[string]any{
			"status": models.AITemplatePublished, "draft_guard": nil, "published_by_id": actorID, "published_at": now,
		}).Error; err != nil {
			return err
		}
		return tx.Model(&parent).Update("status", models.AIContentTemplateActive).Error
	})
	return issues, err
}

func (s *TemplateService) CopyVersion(ctx context.Context, templatePublicID, sourceVersionPublicID string, actorID uint) (*models.AIContentTemplateVersion, error) {
	var copied *models.AIContentTemplateVersion
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var parent models.AIContentTemplate
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("public_id = ?", templatePublicID).First(&parent).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrTemplateNotFound
		} else if err != nil {
			return err
		}
		source, err := getTemplateVersion(tx.Clauses(clause.Locking{Strength: "UPDATE"}), sourceVersionPublicID)
		if err != nil {
			return err
		}
		if source.AIContentTemplateID != parent.ID {
			return ErrTemplateVersionNotFound
		}
		if source.Status != models.AITemplatePublished {
			return ErrTemplateSourceNotPublished
		}
		var draftCount int64
		if err := tx.Model(&models.AIContentTemplateVersion{}).Where("ai_content_template_id = ? AND status = ?", parent.ID, models.AITemplateDraft).Count(&draftCount).Error; err != nil {
			return err
		}
		if draftCount != 0 {
			return ErrTemplateDraftExists
		}
		var maxVersion int
		if err := tx.Model(&models.AIContentTemplateVersion{}).Where("ai_content_template_id = ?", parent.ID).Select("COALESCE(MAX(version_number), 0)").Scan(&maxVersion).Error; err != nil {
			return err
		}
		row := models.AIContentTemplateVersion{
			PublicID: uuid.NewString(), AIContentTemplateID: parent.ID, VersionNumber: maxVersion + 1,
			Status: models.AITemplateDraft, DraftGuard: draftGuardValue(), DefaultLocale: source.DefaultLocale,
			PromptCompilerVersion: source.PromptCompilerVersion, PlatformPrompt: source.PlatformPrompt, CreatedByID: actorID,
		}
		if err := tx.Create(&row).Error; err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "idx_ai_template_draft_guard") || strings.Contains(strings.ToLower(err.Error()), "unique constraint") {
				return ErrTemplateDraftExists
			}
			return err
		}
		for _, sourceSlot := range source.Slots {
			slot := cloneTemplateSlot(sourceSlot, row.ID)
			if err := tx.Create(&slot).Error; err != nil {
				return err
			}
		}
		copied, err = getTemplateVersion(tx, row.PublicID)
		return err
	})
	return copied, err
}

func (s *TemplateService) Archive(ctx context.Context, versionPublicID string) error {
	var identity struct {
		AIContentTemplateID uint
	}
	err := s.db.WithContext(ctx).Model(&models.AIContentTemplateVersion{}).
		Select("ai_content_template_id").Where("public_id = ?", versionPublicID).Take(&identity).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrTemplateVersionNotFound
	}
	if err != nil {
		return err
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var parent models.AIContentTemplate
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&parent, identity.AIContentTemplateID).Error; err != nil {
			return err
		}
		version, err := getTemplateVersion(tx.Clauses(clause.Locking{Strength: "UPDATE"}), versionPublicID)
		if err != nil {
			return err
		}
		if version.AIContentTemplateID != parent.ID {
			return ErrTemplateVersionNotFound
		}
		if version.Status != models.AITemplatePublished {
			return ErrTemplateVersionImmutable
		}
		now := time.Now().UTC()
		if err := tx.Model(version).Updates(map[string]any{"status": models.AITemplateArchived, "draft_guard": nil, "archived_at": now}).Error; err != nil {
			return err
		}
		var publishedCount int64
		if err := tx.Model(&models.AIContentTemplateVersion{}).
			Where("ai_content_template_id = ? AND status = ?", version.AIContentTemplateID, models.AITemplatePublished).
			Count(&publishedCount).Error; err != nil {
			return err
		}
		parentStatus := models.AIContentTemplateArchived
		if publishedCount != 0 {
			parentStatus = models.AIContentTemplateActive
		}
		return tx.Model(&parent).Update("status", parentStatus).Error
	})
}

// DeleteDraft removes only an unpublished draft. Published versions are immutable
// audit records and must be archived instead.
func (s *TemplateService) DeleteDraft(ctx context.Context, versionPublicID string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		version, err := getTemplateVersion(tx.Clauses(clause.Locking{Strength: "UPDATE"}), versionPublicID)
		if err != nil {
			return err
		}
		if version.Status != models.AITemplateDraft {
			return ErrTemplateVersionImmutable
		}
		if err := tx.Where("ai_content_template_version_id = ?", version.ID).Delete(&models.AIContentSlot{}).Error; err != nil {
			return err
		}
		if err := tx.Delete(version).Error; err != nil {
			return err
		}
		var versionCount int64
		if err := tx.Model(&models.AIContentTemplateVersion{}).Where("ai_content_template_id = ?", version.AIContentTemplateID).Count(&versionCount).Error; err != nil {
			return err
		}
		if versionCount == 0 {
			return tx.Delete(&models.AIContentTemplate{}, version.AIContentTemplateID).Error
		}
		return nil
	})
}

func ValidateTemplateVersion(version models.AIContentTemplateVersion, slots []models.AIContentSlot) []ValidationIssue {
	issues := make([]ValidationIssue, 0)
	if strings.TrimSpace(version.DefaultLocale) == "" {
		issues = appendIssue(issues, "default_locale_required", "default_locale", "Default locale is required.")
	}
	if strings.TrimSpace(version.PromptCompilerVersion) == "" {
		issues = appendIssue(issues, "prompt_compiler_version_required", "prompt_compiler_version", "Prompt compiler version is required.")
	}
	issues = append(issues, validatePrompt(version.PlatformPrompt, "platform_prompt", false)...)

	seenKeys := make(map[string]struct{}, len(slots))
	seenSequences := make(map[int]struct{}, len(slots))
	for index, slot := range slots {
		path := fmt.Sprintf("slots[%d]", index)
		key := strings.TrimSpace(slot.SlotKey)
		if key == "" {
			issues = appendIssue(issues, "slot_key_required", path+".slot_key", "Slot key is required.")
		} else if _, exists := seenKeys[key]; exists {
			issues = appendIssue(issues, "slot_key_duplicate", path+".slot_key", "Slot key must be unique within a version.")
		} else {
			seenKeys[key] = struct{}{}
		}
		if !allowedSlotKind(slot.Kind) {
			issues = appendIssue(issues, "slot_kind_invalid", path+".kind", "Slot kind is not supported.")
		}
		if strings.TrimSpace(slot.NameZH) == "" {
			issues = appendIssue(issues, "slot_name_zh_required", path+".name_zh", "Chinese slot name is required.")
		}
		if strings.TrimSpace(slot.NameEN) == "" {
			issues = appendIssue(issues, "slot_name_en_required", path+".name_en", "English slot name is required.")
		}
		if slot.Sequence < 1 || slot.Sequence > len(slots) {
			issues = appendIssue(issues, "slot_sequence_invalid", path+".sequence", "Slot sequences must be contiguous starting at one.")
		} else if _, exists := seenSequences[slot.Sequence]; exists {
			issues = appendIssue(issues, "slot_sequence_invalid", path+".sequence", "Slot sequences must be unique and contiguous.")
		} else {
			seenSequences[slot.Sequence] = struct{}{}
		}
		issues = append(issues, validatePrompt(slot.PromptFragment, path+".prompt_fragment", true)...)

		constraints, constraintIssues := decodeJSONObject(slot.ConstraintsJSON, path+".constraints", "constraints_object_required")
		generation, generationIssues := decodeJSONObject(slot.GenerationConfigJSON, path+".generation_config", "generation_config_object_required")
		layout, layoutIssues := decodeJSONObject(slot.LayoutConfigJSON, path+".layout_config", "layout_config_object_required")
		issues = append(issues, constraintIssues...)
		issues = append(issues, generationIssues...)
		issues = append(issues, layoutIssues...)
		for _, config := range []map[string]any{constraints, generation} {
			if config != nil {
				issues = append(issues, validateCandidateCount(config, path)...)
			}
		}
		if generation != nil {
			issues = append(issues, validateGenerationAllowLists(generation, slot.Kind, path+".generation_config")...)
			if value, exists := generation["allow_user_extra_prompt"]; exists {
				if _, ok := value.(bool); !ok {
					issues = appendIssue(issues, "allow_user_extra_prompt_invalid", path+".generation_config.allow_user_extra_prompt", "User extra prompt permission must be a boolean.")
				}
			}
		}
		if slot.Kind == models.AIContentSlotImage {
			issues = append(issues, validateImageSize(generation, path+".generation_config.size")...)
			issues = append(issues, validateRequiredViews(constraints, path+".constraints.required_views")...)
		} else if constraints != nil {
			rawConstraints, _ := json.Marshal(constraints)
			if _, err := parseTextConstraintRules(rawConstraints, slot.Kind); err != nil {
				issues = appendIssue(issues, "text_constraints_invalid", path+".constraints", "Text constraints contain unsupported or invalid rules.")
			}
		}
		for _, config := range []map[string]any{generation, layout} {
			if config != nil {
				issues = append(issues, validateSafeArea(config, path)...)
			}
		}
	}
	return issues
}

func validateRequiredViews(config map[string]any, path string) []ValidationIssue {
	if config == nil {
		return nil
	}
	value, exists := config["required_views"]
	if !exists {
		return nil
	}
	items, ok := value.([]any)
	if !ok {
		return []ValidationIssue{{Code: "required_views_invalid", Path: path, Message: "Required views must be an array of non-empty strings."}}
	}
	for _, item := range items {
		text, ok := item.(string)
		if !ok || strings.TrimSpace(text) == "" {
			return []ValidationIssue{{Code: "required_views_invalid", Path: path, Message: "Required views must be an array of non-empty strings."}}
		}
	}
	return nil
}

func newTemplateVersion(templateID uint, versionNumber int, actorID uint, input UpdateTemplateVersionInput) models.AIContentTemplateVersion {
	return models.AIContentTemplateVersion{
		PublicID: uuid.NewString(), AIContentTemplateID: templateID, VersionNumber: versionNumber,
		Status: models.AITemplateDraft, DraftGuard: draftGuardValue(), DefaultLocale: normalizedDefault(input.DefaultLocale, defaultTemplateLocale),
		PromptCompilerVersion: normalizedDefault(input.PromptCompilerVersion, defaultPromptCompilerVersion),
		PlatformPrompt:        strings.TrimSpace(input.PlatformPrompt), CreatedByID: actorID,
	}
}

func normalizeSlotInputs(versionID uint, inputs []SlotInput) []models.AIContentSlot {
	ordered := append([]SlotInput(nil), inputs...)
	hasExplicitSequence := false
	for _, input := range ordered {
		if input.Sequence > 0 {
			hasExplicitSequence = true
			break
		}
	}
	if hasExplicitSequence {
		sort.SliceStable(ordered, func(i, j int) bool {
			if ordered[i].Sequence == ordered[j].Sequence {
				return i < j
			}
			return ordered[i].Sequence < ordered[j].Sequence
		})
	}
	result := make([]models.AIContentSlot, 0, len(ordered))
	for index, input := range ordered {
		sequence := input.Sequence
		if !hasExplicitSequence {
			sequence = index + 1
		}
		result = append(result, models.AIContentSlot{
			PublicID: uuid.NewString(), AIContentTemplateVersionID: versionID,
			SlotKey: strings.TrimSpace(input.SlotKey), Kind: models.AIContentSlotKind(strings.TrimSpace(input.Kind)),
			NameZH: strings.TrimSpace(input.NameZH), NameEN: strings.TrimSpace(input.NameEN),
			DescriptionZH: strings.TrimSpace(input.DescriptionZH), DescriptionEN: strings.TrimSpace(input.DescriptionEN),
			Sequence: sequence, Optional: input.Optional, DefaultSelected: input.DefaultSelected,
			PromptFragment:  strings.TrimSpace(input.PromptFragment),
			ConstraintsJSON: normalizeJSONObject(input.Constraints), GenerationConfigJSON: normalizeJSONObject(input.GenerationConfig), LayoutConfigJSON: normalizeJSONObject(input.LayoutConfig),
		})
	}
	return result
}

func getTemplateVersion(db *gorm.DB, publicID string) (*models.AIContentTemplateVersion, error) {
	var version models.AIContentTemplateVersion
	err := db.Preload("Slots", func(db *gorm.DB) *gorm.DB { return db.Order("sequence ASC, id ASC") }).Where("public_id = ?", publicID).First(&version).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrTemplateVersionNotFound
	}
	if err != nil {
		return nil, err
	}
	return &version, nil
}

func cloneTemplateSlot(source models.AIContentSlot, versionID uint) models.AIContentSlot {
	return models.AIContentSlot{
		PublicID: uuid.NewString(), AIContentTemplateVersionID: versionID, SlotKey: source.SlotKey, Kind: source.Kind,
		NameZH: source.NameZH, NameEN: source.NameEN, DescriptionZH: source.DescriptionZH, DescriptionEN: source.DescriptionEN,
		Sequence: source.Sequence, Optional: source.Optional, DefaultSelected: source.DefaultSelected, PromptFragment: source.PromptFragment,
		ConstraintsJSON: append([]byte(nil), source.ConstraintsJSON...), GenerationConfigJSON: append([]byte(nil), source.GenerationConfigJSON...), LayoutConfigJSON: append([]byte(nil), source.LayoutConfigJSON...),
	}
}

func validateTemplateNames(template models.AIContentTemplate) []ValidationIssue {
	var issues []ValidationIssue
	if strings.TrimSpace(template.NameZH) == "" {
		issues = appendIssue(issues, "name_zh_required", "name_zh", "Chinese template name is required.")
	}
	if strings.TrimSpace(template.NameEN) == "" {
		issues = appendIssue(issues, "name_en_required", "name_en", "English template name is required.")
	}
	if strings.TrimSpace(template.TargetPlatform) == "" {
		issues = appendIssue(issues, "target_platform_required", "target_platform", "Target platform is required.")
	}
	return issues
}

var templateVariablePattern = regexp.MustCompile(`\{\{\s*([a-zA-Z0-9_.-]+)\s*\}\}`)

func validatePrompt(prompt, path string, required bool) []ValidationIssue {
	trimmed := strings.TrimSpace(prompt)
	var issues []ValidationIssue
	if required && trimmed == "" {
		return appendIssue(issues, "prompt_required", path, "Prompt fragment is required.")
	}
	if looksLikeSecret(trimmed) {
		issues = appendIssue(issues, "prompt_secret_forbidden", path, "Prompt content appears to contain a secret.")
	}
	for _, match := range templateVariablePattern.FindAllStringSubmatch(trimmed, -1) {
		if !knownTemplateVariable(match[1]) {
			issues = appendIssue(issues, "template_variable_unknown", path, fmt.Sprintf("Template variable %q is not supported.", match[1]))
		}
	}
	return issues
}

var supportedTemplateVariables = map[string]struct{}{
	"locale": {}, "target_platform": {}, "candidate_count": {},
	"product.name": {}, "product.brand": {}, "product.category": {}, "product.description": {}, "product.product_type": {},
	"sku.code": {}, "sku.color": {}, "sku.size": {}, "sku.platform_title": {}, "sku.attributes": {},
	"sop.name_zh": {}, "sop.name_en": {}, "sop.version": {}, "sop.coordinate_system": {}, "sop.required_views": {}, "sop.views": {},
	"style.name": {}, "style.description": {}, "style.instructions": {}, "style.preferences": {},
	"approved_assets": {}, "approved_assets.metadata": {},
}

func knownTemplateVariable(variable string) bool {
	_, ok := supportedTemplateVariables[variable]
	return ok
}

func draftGuardValue() *string {
	guard := templateDraftGuard
	return &guard
}

func looksLikeSecret(value string) bool {
	lower := strings.ToLower(value)
	return strings.Contains(lower, "sk-proj-") || strings.Contains(lower, "api_key=") || strings.Contains(lower, "authorization: bearer ") || strings.Contains(lower, "-----begin private key-----")
}

func decodeJSONObject(raw []byte, path, code string) (map[string]any, []ValidationIssue) {
	var object map[string]any
	if len(raw) == 0 || json.Unmarshal(raw, &object) != nil || object == nil {
		return nil, []ValidationIssue{{Code: code, Path: path, Message: "Configuration must be a JSON object."}}
	}
	return object, nil
}

func validateCandidateCount(config map[string]any, path string) []ValidationIssue {
	value, exists := config["candidate_count"]
	if !exists {
		return nil
	}
	number, ok := value.(float64)
	if !ok || number != float64(int(number)) || number < 1 || number > 4 {
		return []ValidationIssue{{Code: "candidate_count_invalid", Path: path + ".candidate_count", Message: "Candidate count must be an integer from 1 to 4."}}
	}
	return nil
}

func validateImageSize(config map[string]any, path string) []ValidationIssue {
	value, exists := config["size"]
	if !exists {
		return nil
	}
	size, ok := value.(string)
	if !ok {
		return []ValidationIssue{{Code: "image_size_invalid", Path: path, Message: "Image size is not supported."}}
	}
	if supportedImageSize(size) {
		return nil
	}
	return []ValidationIssue{{Code: "image_size_invalid", Path: path, Message: "Image size is not supported."}}
}

func validateGenerationAllowLists(config map[string]any, kind models.AIContentSlotKind, path string) []ValidationIssue {
	var issues []ValidationIssue
	if value, ok := config["allowed_candidate_count"]; ok && !validNumberAllowList(value, 1, 4) {
		issues = appendIssue(issues, "allowed_candidate_count_invalid", path+".allowed_candidate_count", "Allowed candidate counts must be unique integers from 1 to 4.")
	}
	imageOnly := []struct {
		key, code, message string
		valid              func(string) bool
	}{{"allowed_sizes", "allowed_sizes_invalid", "Allowed sizes must be a unique non-empty list of supported image sizes.", supportedImageSize}, {"allowed_qualities", "allowed_qualities_invalid", "Allowed qualities must be a unique non-empty list using low, medium, high, or auto.", supportedQuality}, {"allowed_styles", "allowed_styles_invalid", "Allowed styles must be unique, trimmed, non-empty, at most 80 characters, and at most 20 entries.", func(v string) bool { return strings.TrimSpace(v) == v && v != "" && utf8.RuneCountInString(v) <= 80 }}}
	for _, rule := range imageOnly {
		value, exists := config[rule.key]
		if !exists {
			continue
		}
		if kind != models.AIContentSlotImage || !validStringAllowList(value, rule.valid) {
			issues = appendIssue(issues, rule.code, path+"."+rule.key, rule.message)
		}
	}
	return issues
}
func validNumberAllowList(value any, min, max int) bool {
	items, ok := value.([]any)
	if !ok || len(items) == 0 {
		return false
	}
	seen := map[int]bool{}
	for _, item := range items {
		number, ok := item.(float64)
		integer := int(number)
		if !ok || number != float64(integer) || integer < min || integer > max || seen[integer] {
			return false
		}
		seen[integer] = true
	}
	return true
}
func validStringAllowList(value any, valid func(string) bool) bool {
	items, ok := value.([]any)
	if !ok || len(items) == 0 || len(items) > 20 {
		return false
	}
	seen := map[string]bool{}
	for _, item := range items {
		text, ok := item.(string)
		if !ok || !valid(text) || seen[text] {
			return false
		}
		seen[text] = true
	}
	return true
}

func validateSafeArea(config map[string]any, path string) []ValidationIssue {
	value, exists := config["text_safe_area"]
	if !exists {
		return nil
	}
	area, ok := value.(map[string]any)
	if !ok {
		return []ValidationIssue{{Code: "safe_area_invalid", Path: path + ".text_safe_area", Message: "Text safe area must use normalized bounds."}}
	}
	x, xOK := jsonNumber(area["x"])
	y, yOK := jsonNumber(area["y"])
	width, widthOK := jsonNumber(area["width"])
	height, heightOK := jsonNumber(area["height"])
	if !xOK || !yOK || !widthOK || !heightOK || x < 0 || y < 0 || width <= 0 || height <= 0 || x+width > 1 || y+height > 1 {
		return []ValidationIssue{{Code: "safe_area_invalid", Path: path + ".text_safe_area", Message: "Text safe area must use normalized bounds."}}
	}
	return nil
}

func jsonNumber(value any) (float64, bool) {
	number, ok := value.(float64)
	return number, ok
}

func allowedSlotKind(kind models.AIContentSlotKind) bool {
	return kind == models.AIContentSlotImage || kind == models.AIContentSlotTitle || kind == models.AIContentSlotSEODescription
}

func normalizeJSONObject(raw json.RawMessage) []byte {
	if len(strings.TrimSpace(string(raw))) == 0 {
		return []byte("{}")
	}
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return append([]byte(nil), raw...)
	}
	normalized, err := json.Marshal(value)
	if err != nil {
		return append([]byte(nil), raw...)
	}
	return normalized
}

func normalizedDefault(value, fallback string) string {
	if value = strings.TrimSpace(value); value != "" {
		return value
	}
	return fallback
}

func appendIssue(issues []ValidationIssue, code, path, message string) []ValidationIssue {
	return append(issues, ValidationIssue{Code: code, Path: path, Message: message})
}
