package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"cargoflow/api/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

const ProductSnapshotSchemaV1 = "cargoflow_product_generation_v1"

var (
	ErrJobNotFound                 = errors.New("AI job not found")
	ErrSKUNotFound                 = errors.New("SKU not found")
	ErrTemplateVersionNotPublished = errors.New("AI content template version is not published")
	ErrSlotSelectionInvalid        = errors.New("selected slots are invalid")
	ErrAssetNotEligible            = errors.New("selected assets must be approved and belong to the exact SKU")
	ErrPublishedSOPNotFound        = errors.New("published capture SOP not found for SKU category")
)

type CreateJobInput struct {
	SKUID                   uint
	TemplateVersionPublicID string
	SelectedSlotKeys        []string
	SelectedAssetIDs        []uint
	Locale                  string
	CreatedByID             uint
}

type LocalizedNameFacts struct {
	ZH string `json:"zh"`
	EN string `json:"en"`
}

type CategoryFacts struct {
	NameZH string `json:"name_zh"`
	NameEN string `json:"name_en"`
}

type ProductFacts struct {
	Name        string        `json:"name"`
	Brand       string        `json:"brand"`
	Description string        `json:"description"`
	Category    CategoryFacts `json:"category"`
}

type SKUFacts struct {
	Code          string   `json:"code"`
	Color         string   `json:"color"`
	Size          string   `json:"size"`
	PlatformTitle string   `json:"platform_title"`
	SellingPoints string   `json:"selling_points"`
	Tags          []string `json:"tags"`
}

type VectorFacts struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	Z float64 `json:"z"`
}

type SOPViewFacts struct {
	PublicID                string             `json:"public_id"`
	Sequence                int                `json:"sequence"`
	Role                    models.SOPViewRole `json:"role"`
	ViewKind                models.SOPViewKind `json:"view_kind"`
	PresetKey               string             `json:"preset_key"`
	Name                    LocalizedNameFacts `json:"name"`
	Instruction             LocalizedNameFacts `json:"instruction"`
	Required                bool               `json:"required"`
	CameraPositionDirection VectorFacts        `json:"camera_position_direction"`
	ImageUpDirection        VectorFacts        `json:"image_up_direction"`
	Target                  VectorFacts        `json:"target"`
	Composition             models.Composition `json:"composition"`
}

type SOPFacts struct {
	PublicID         string             `json:"public_id"`
	VersionPublicID  string             `json:"version_public_id"`
	VersionNumber    int                `json:"version_number"`
	SchemaVersion    string             `json:"schema_version"`
	Name             LocalizedNameFacts `json:"name"`
	Description      LocalizedNameFacts `json:"description"`
	CoordinateSystem string             `json:"coordinate_system"`
	Views            []SOPViewFacts     `json:"views"`
}

type AssetFacts struct {
	ID           uint           `json:"id"`
	ObjectKey    string         `json:"object_key"`
	OriginalURL  string         `json:"original_url"`
	ThumbnailURL string         `json:"thumbnail_url"`
	CapturedAt   time.Time      `json:"captured_at"`
	View         AssetViewFacts `json:"view"`
}

type AssetViewFacts struct {
	PublicID  string             `json:"public_id"`
	PresetKey string             `json:"preset_key"`
	Name      LocalizedNameFacts `json:"name"`
	Role      models.SOPViewRole `json:"role"`
	ViewKind  models.SOPViewKind `json:"view_kind"`
}

type SlotFacts struct {
	PublicID         string                   `json:"public_id"`
	SlotKey          string                   `json:"slot_key"`
	Kind             models.AIContentSlotKind `json:"kind"`
	Name             LocalizedNameFacts       `json:"name"`
	Description      LocalizedNameFacts       `json:"description"`
	Sequence         int                      `json:"sequence"`
	Optional         bool                     `json:"optional"`
	DefaultSelected  bool                     `json:"default_selected"`
	PromptFragment   string                   `json:"prompt_fragment"`
	Constraints      json.RawMessage          `json:"constraints"`
	GenerationConfig json.RawMessage          `json:"generation_config"`
	LayoutConfig     json.RawMessage          `json:"layout_config"`
}

type TemplateFacts struct {
	TemplatePublicID      string      `json:"template_public_id"`
	VersionPublicID       string      `json:"version_public_id"`
	VersionNumber         int         `json:"version_number"`
	PromptCompilerVersion string      `json:"prompt_compiler_version"`
	PlatformPrompt        string      `json:"platform_prompt"`
	SelectedSlots         []SlotFacts `json:"selected_slots"`
}

type ProductSnapshotV1 struct {
	Schema         string        `json:"schema"`
	Locale         string        `json:"locale"`
	TargetPlatform string        `json:"target_platform"`
	Product        ProductFacts  `json:"product"`
	SKU            SKUFacts      `json:"sku"`
	SOP            SOPFacts      `json:"sop"`
	Template       TemplateFacts `json:"template"`
	SelectedAssets []AssetFacts  `json:"selected_assets"`
}

type JobItemDocument struct {
	PublicID              string                   `json:"public_id"`
	SlotKey               string                   `json:"slot_key"`
	Kind                  models.AIContentSlotKind `json:"kind"`
	Status                models.AIJobItemStatus   `json:"status"`
	SlotSnapshot          json.RawMessage          `json:"slot_snapshot"`
	SelectedInputAssetIDs []uint                   `json:"selected_input_asset_ids"`
	AttemptCount          int                      `json:"attempt_count"`
	SafeError             string                   `json:"safe_error"`
	StartedAt             *time.Time               `json:"started_at"`
	CompletedAt           *time.Time               `json:"completed_at"`
	CreatedAt             time.Time                `json:"created_at"`
	UpdatedAt             time.Time                `json:"updated_at"`
}

type JobDocument struct {
	PublicID                string             `json:"public_id"`
	SKUID                   uint               `json:"sku_id"`
	TemplateVersionPublicID string             `json:"template_version_id"`
	TargetPlatform          string             `json:"target_platform"`
	Locale                  string             `json:"locale"`
	Status                  models.AIJobStatus `json:"status"`
	SnapshotSchema          string             `json:"snapshot_schema"`
	InputSnapshot           json.RawMessage    `json:"input_snapshot"`
	StartedAt               *time.Time         `json:"started_at"`
	CompletedAt             *time.Time         `json:"completed_at"`
	CancelledAt             *time.Time         `json:"cancelled_at"`
	CreatedAt               time.Time          `json:"created_at"`
	UpdatedAt               time.Time          `json:"updated_at"`
	Items                   []JobItemDocument  `json:"items"`
}

type JobService struct{ db *gorm.DB }

func NewJobService(db *gorm.DB) *JobService { return &JobService{db: db} }

func (s *JobService) Create(ctx context.Context, input CreateJobInput) (JobDocument, error) {
	var result JobDocument
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		sku, err := loadJobSKU(tx, input.SKUID)
		if err != nil {
			return err
		}
		version, template, err := loadPublishedTemplateVersion(tx, input.TemplateVersionPublicID)
		if err != nil {
			return err
		}
		selectedSlots, err := selectJobSlots(version.Slots, input.SelectedSlotKeys)
		if err != nil {
			return err
		}
		assets, assetIDs, err := loadEligibleAssets(tx, input.SKUID, input.SelectedAssetIDs)
		if err != nil {
			return err
		}
		if err := validateImageSlotAssets(selectedSlots, assets); err != nil {
			return err
		}
		sop, captureSOPPublicID, err := loadPublishedSOP(tx, sku.Product.CategoryID)
		if err != nil {
			return err
		}
		locale := strings.TrimSpace(input.Locale)
		if locale == "" {
			locale = version.DefaultLocale
		}
		snapshot := makeProductSnapshot(sku, sop, captureSOPPublicID, template, version, selectedSlots, assets, locale)
		snapshotJSON, err := json.Marshal(snapshot)
		if err != nil {
			return fmt.Errorf("marshal AI job snapshot: %w", err)
		}
		job := models.AIJob{PublicID: uuid.NewString(), SKUID: sku.ID, AIContentTemplateVersionID: version.ID, TargetPlatform: template.TargetPlatform, Locale: locale, Status: models.AIJobQueued, SnapshotSchema: ProductSnapshotSchemaV1, InputSnapshotJSON: snapshotJSON, CreatedByID: input.CreatedByID}
		if err := tx.Create(&job).Error; err != nil {
			return fmt.Errorf("create AI job: %w", err)
		}
		items := make([]models.AIJobItem, 0, len(selectedSlots))
		for _, slot := range selectedSlots {
			slotSnapshot, err := json.Marshal(slotFacts(slot))
			if err != nil {
				return fmt.Errorf("marshal AI job slot: %w", err)
			}
			ids := []uint{}
			if slot.Kind == models.AIContentSlotImage {
				ids = assetIDs
			}
			idsJSON, err := json.Marshal(ids)
			if err != nil {
				return fmt.Errorf("marshal AI job assets: %w", err)
			}
			item := models.AIJobItem{PublicID: uuid.NewString(), AIJobID: job.ID, AIContentSlotID: slot.ID, SlotKey: slot.SlotKey, SlotSnapshotJSON: slotSnapshot, Kind: slot.Kind, Status: models.AIJobItemQueued, SelectedInputAssetIDsJSON: idsJSON}
			if err := tx.Create(&item).Error; err != nil {
				return fmt.Errorf("create AI job item: %w", err)
			}
			items = append(items, item)
		}
		job.Items = items
		result = jobDocument(job, version.PublicID)
		return nil
	})
	return result, err
}

func (s *JobService) List(ctx context.Context) ([]JobDocument, error) {
	var jobs []models.AIJob
	if err := s.db.WithContext(ctx).Preload("Items", func(db *gorm.DB) *gorm.DB { return db.Order("created_at ASC, id ASC") }).Order("created_at DESC, id DESC").Find(&jobs).Error; err != nil {
		return nil, err
	}
	versionIDs := make([]uint, 0, len(jobs))
	for _, job := range jobs {
		versionIDs = append(versionIDs, job.AIContentTemplateVersionID)
	}
	publicIDs, err := versionPublicIDs(s.db.WithContext(ctx), versionIDs)
	if err != nil {
		return nil, err
	}
	result := make([]JobDocument, 0, len(jobs))
	for _, job := range jobs {
		result = append(result, jobDocument(job, publicIDs[job.AIContentTemplateVersionID]))
	}
	return result, nil
}

func (s *JobService) Get(ctx context.Context, publicID string) (JobDocument, error) {
	var job models.AIJob
	if err := s.db.WithContext(ctx).Preload("Items", func(db *gorm.DB) *gorm.DB { return db.Order("created_at ASC, id ASC") }).Where("public_id = ?", publicID).First(&job).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return JobDocument{}, ErrJobNotFound
		}
		return JobDocument{}, err
	}
	ids, err := versionPublicIDs(s.db.WithContext(ctx), []uint{job.AIContentTemplateVersionID})
	if err != nil {
		return JobDocument{}, err
	}
	return jobDocument(job, ids[job.AIContentTemplateVersionID]), nil
}

func loadJobSKU(tx *gorm.DB, id uint) (models.SKU, error) {
	var sku models.SKU
	err := tx.Preload("Product.CatalogCategory").Preload("Tags", func(db *gorm.DB) *gorm.DB { return db.Order("tags.name ASC, tags.id ASC") }).First(&sku, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return sku, ErrSKUNotFound
	}
	return sku, err
}

func loadPublishedTemplateVersion(tx *gorm.DB, publicID string) (models.AIContentTemplateVersion, models.AIContentTemplate, error) {
	var version models.AIContentTemplateVersion
	if err := tx.Preload("Slots", func(db *gorm.DB) *gorm.DB { return db.Order("sequence ASC, id ASC") }).Where("public_id = ?", publicID).First(&version).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return version, models.AIContentTemplate{}, ErrTemplateVersionNotFound
		}
		return version, models.AIContentTemplate{}, err
	}
	if version.Status != models.AITemplatePublished {
		return version, models.AIContentTemplate{}, ErrTemplateVersionNotPublished
	}
	var template models.AIContentTemplate
	if err := tx.First(&template, version.AIContentTemplateID).Error; err != nil {
		return version, template, err
	}
	return version, template, nil
}

func selectJobSlots(slots []models.AIContentSlot, keys []string) ([]models.AIContentSlot, error) {
	if len(keys) == 0 {
		return nil, ErrSlotSelectionInvalid
	}
	requested := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		if key == "" {
			return nil, ErrSlotSelectionInvalid
		}
		if _, exists := requested[key]; exists {
			return nil, ErrSlotSelectionInvalid
		}
		requested[key] = struct{}{}
	}
	selected := make([]models.AIContentSlot, 0, len(keys))
	for _, slot := range slots {
		if _, ok := requested[slot.SlotKey]; ok {
			selected = append(selected, slot)
			delete(requested, slot.SlotKey)
		}
	}
	if len(requested) != 0 {
		return nil, ErrSlotSelectionInvalid
	}
	return selected, nil
}

func loadEligibleAssets(tx *gorm.DB, skuID uint, requested []uint) ([]models.Asset, []uint, error) {
	set := make(map[uint]struct{}, len(requested))
	for _, id := range requested {
		set[id] = struct{}{}
	}
	ids := make([]uint, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	if len(ids) == 0 {
		return []models.Asset{}, []uint{}, nil
	}
	var assets []models.Asset
	if err := tx.Preload("SOPView").Where("id IN ?", ids).Where(&models.Asset{SKUID: skuID, ReviewStatus: "approved"}).Order("id ASC").Find(&assets).Error; err != nil {
		return nil, nil, err
	}
	if len(assets) != len(ids) {
		return nil, nil, ErrAssetNotEligible
	}
	return assets, ids, nil
}

func validateImageSlotAssets(slots []models.AIContentSlot, assets []models.Asset) error {
	availableViews := make(map[string]struct{}, len(assets))
	for _, asset := range assets {
		availableViews[asset.SOPView.PresetKey] = struct{}{}
	}
	for _, slot := range slots {
		if slot.Kind != models.AIContentSlotImage {
			continue
		}
		if len(assets) == 0 {
			return ErrAssetNotEligible
		}
		var constraints struct {
			RequiredViews []string `json:"required_views"`
		}
		if len(slot.ConstraintsJSON) != 0 && json.Unmarshal(slot.ConstraintsJSON, &constraints) == nil {
			for _, view := range constraints.RequiredViews {
				if _, ok := availableViews[view]; !ok {
					return ErrAssetNotEligible
				}
			}
		}
	}
	return nil
}

func loadPublishedSOP(tx *gorm.DB, categoryID uint) (models.SOPVersion, string, error) {
	var version models.SOPVersion
	err := tx.Preload("Views", func(db *gorm.DB) *gorm.DB { return db.Order("sequence ASC, id ASC") }).Joins("JOIN capture_sops ON capture_sops.id = sop_versions.capture_sop_id").Where("capture_sops.category_id = ? AND sop_versions.status = ?", categoryID, models.SOPVersionPublished).Order("sop_versions.version_number DESC, sop_versions.id DESC").First(&version).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return version, "", ErrPublishedSOPNotFound
	}
	if err != nil {
		return version, "", err
	}
	var sop models.CaptureSOP
	if err := tx.Select("public_id").First(&sop, version.CaptureSOPID).Error; err != nil {
		return version, "", err
	}
	return version, sop.PublicID, nil
}

func makeProductSnapshot(sku models.SKU, sop models.SOPVersion, captureSOPPublicID string, template models.AIContentTemplate, version models.AIContentTemplateVersion, slots []models.AIContentSlot, assets []models.Asset, locale string) ProductSnapshotV1 {
	tags := make([]string, 0, len(sku.Tags))
	for _, tag := range sku.Tags {
		tags = append(tags, tag.Name)
	}
	views := make([]SOPViewFacts, 0, len(sop.Views))
	for _, view := range sop.Views {
		views = append(views, SOPViewFacts{PublicID: view.PublicID, Sequence: view.Sequence, Role: view.Role, ViewKind: view.ViewKind, PresetKey: view.PresetKey, Name: LocalizedNameFacts{ZH: view.NameZH, EN: view.NameEN}, Instruction: LocalizedNameFacts{ZH: view.InstructionZH, EN: view.InstructionEN}, Required: view.Required, CameraPositionDirection: VectorFacts{X: view.CameraPositionX, Y: view.CameraPositionY, Z: view.CameraPositionZ}, ImageUpDirection: VectorFacts{X: view.ImageUpX, Y: view.ImageUpY, Z: view.ImageUpZ}, Target: VectorFacts{X: view.TargetX, Y: view.TargetY, Z: view.TargetZ}, Composition: view.Composition})
	}
	assetFacts := make([]AssetFacts, 0, len(assets))
	for _, asset := range assets {
		assetFacts = append(assetFacts, AssetFacts{ID: asset.ID, ObjectKey: asset.ObjectKey, OriginalURL: asset.OriginalURL, ThumbnailURL: asset.ThumbnailURL, CapturedAt: asset.CapturedAt, View: AssetViewFacts{PublicID: asset.SOPView.PublicID, PresetKey: asset.SOPView.PresetKey, Name: LocalizedNameFacts{ZH: asset.SOPView.NameZH, EN: asset.SOPView.NameEN}, Role: asset.SOPView.Role, ViewKind: asset.SOPView.ViewKind}})
	}
	selectedSlots := make([]SlotFacts, 0, len(slots))
	for _, slot := range slots {
		selectedSlots = append(selectedSlots, slotFacts(slot))
	}
	return ProductSnapshotV1{Schema: ProductSnapshotSchemaV1, Locale: locale, TargetPlatform: template.TargetPlatform, Product: ProductFacts{Name: sku.Product.Name, Brand: sku.Product.Brand, Description: sku.Product.Description, Category: CategoryFacts{NameZH: sku.Product.CatalogCategory.Name, NameEN: sku.Product.CatalogCategory.NameEN}}, SKU: SKUFacts{Code: sku.Code, Color: sku.Color, Size: sku.Size, PlatformTitle: sku.PlatformTitle, SellingPoints: sku.SellingPoints, Tags: tags}, SOP: SOPFacts{PublicID: captureSOPPublicID, VersionPublicID: sop.PublicID, VersionNumber: sop.VersionNumber, SchemaVersion: sop.SchemaVersion, Name: LocalizedNameFacts{ZH: sop.NameZH, EN: sop.NameEN}, Description: LocalizedNameFacts{ZH: sop.DescriptionZH, EN: sop.DescriptionEN}, CoordinateSystem: sop.CoordinateSystem, Views: views}, Template: TemplateFacts{TemplatePublicID: template.PublicID, VersionPublicID: version.PublicID, VersionNumber: version.VersionNumber, PromptCompilerVersion: version.PromptCompilerVersion, PlatformPrompt: version.PlatformPrompt, SelectedSlots: selectedSlots}, SelectedAssets: assetFacts}
}

func slotFacts(slot models.AIContentSlot) SlotFacts {
	return SlotFacts{PublicID: slot.PublicID, SlotKey: slot.SlotKey, Kind: slot.Kind, Name: LocalizedNameFacts{ZH: slot.NameZH, EN: slot.NameEN}, Description: LocalizedNameFacts{ZH: slot.DescriptionZH, EN: slot.DescriptionEN}, Sequence: slot.Sequence, Optional: slot.Optional, DefaultSelected: slot.DefaultSelected, PromptFragment: slot.PromptFragment, Constraints: cloneJSON(slot.ConstraintsJSON), GenerationConfig: cloneJSON(slot.GenerationConfigJSON), LayoutConfig: cloneJSON(slot.LayoutConfigJSON)}
}
func cloneJSON(value []byte) json.RawMessage {
	if len(value) == 0 {
		return json.RawMessage(`{}`)
	}
	return append(json.RawMessage(nil), value...)
}

func versionPublicIDs(db *gorm.DB, ids []uint) (map[uint]string, error) {
	result := map[uint]string{}
	if len(ids) == 0 {
		return result, nil
	}
	var rows []struct {
		ID       uint
		PublicID string
	}
	if err := db.Model(&models.AIContentTemplateVersion{}).Select("id", "public_id").Where("id IN ?", ids).Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		result[row.ID] = row.PublicID
	}
	return result, nil
}

func jobDocument(job models.AIJob, versionPublicID string) JobDocument {
	items := make([]JobItemDocument, 0, len(job.Items))
	for _, item := range job.Items {
		ids := []uint{}
		_ = json.Unmarshal(item.SelectedInputAssetIDsJSON, &ids)
		items = append(items, JobItemDocument{PublicID: item.PublicID, SlotKey: item.SlotKey, Kind: item.Kind, Status: item.Status, SlotSnapshot: cloneJSON(item.SlotSnapshotJSON), SelectedInputAssetIDs: ids, AttemptCount: item.AttemptCount, SafeError: item.SafeError, StartedAt: item.StartedAt, CompletedAt: item.CompletedAt, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt})
	}
	return JobDocument{PublicID: job.PublicID, SKUID: job.SKUID, TemplateVersionPublicID: versionPublicID, TargetPlatform: job.TargetPlatform, Locale: job.Locale, Status: job.Status, SnapshotSchema: job.SnapshotSchema, InputSnapshot: append(json.RawMessage(nil), job.InputSnapshotJSON...), StartedAt: job.StartedAt, CompletedAt: job.CompletedAt, CancelledAt: job.CancelledAt, CreatedAt: job.CreatedAt, UpdatedAt: job.UpdatedAt, Items: items}
}
