package sop

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"sort"
	"strings"
	"time"

	"cargoflow/api/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrVariantManifestNotFound           = errors.New("variant identity manifest not found")
	ErrVariantManifestVersionNotFound    = errors.New("variant identity manifest version not found")
	ErrVariantManifestDraftExists        = errors.New("a variant identity manifest draft already exists")
	ErrVariantManifestImmutable          = errors.New("variant identity manifest version is immutable")
	ErrVariantManifestInvalid            = errors.New("variant identity manifest is invalid")
	ErrVariantManifestValidation         = errors.New("variant identity manifest validation failed")
	ErrVariantManifestSourceNotPublished = errors.New("source variant identity manifest version is not published")
)

const variantIdentitySchemaV1 = "variant_identity_v1"

var hexColor = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

// VariantIdentityDocumentV1 is the structured, target-SKU-only identity
// evidence used by later SOP and generation layers. It intentionally has no
// free-form object locator or source URL fields.
type VariantIdentityDocumentV1 struct {
	Schema                    string               `json:"schema"`
	Colors                    []VariantColorRegion `json:"colors"`
	Material                  string               `json:"material"`
	Finish                    string               `json:"finish"`
	Texture                   string               `json:"texture"`
	Labels                    []VariantLabel       `json:"labels"`
	Ports                     []VariantFeature     `json:"ports"`
	Controls                  []VariantFeature     `json:"controls"`
	Accessories               []string             `json:"accessories"`
	Packaging                 []VariantFeature     `json:"packaging"`
	Other                     []VariantFeature     `json:"other"`
	MustProveWithTargetAssets []string             `json:"must_prove_with_target_assets"`
}

type VariantColorRegion struct {
	Key   string `json:"key"`
	Name  string `json:"name"`
	Value string `json:"value,omitempty"`
}

type VariantLabel struct {
	Key       string `json:"key"`
	Text      string `json:"text"`
	RegionKey string `json:"region_key,omitempty"`
}

type VariantFeature struct {
	Key         string `json:"key"`
	Description string `json:"description"`
	RegionKey   string `json:"region_key,omitempty"`
}

type VariantDifferenceRegionInput struct {
	Key                  string                            `json:"key"`
	DifferenceKind       models.DifferenceKind             `json:"difference_kind"`
	Strictness           models.DifferenceRegionStrictness `json:"strictness"`
	DescriptionZH        string                            `json:"description_zh"`
	DescriptionEN        string                            `json:"description_en"`
	Shape                json.RawMessage                   `json:"shape"`
	ForbiddenInheritance []string                          `json:"forbidden_inheritance"`
	RequiredViewKeys     []string                          `json:"required_view_keys"`
	EvidenceAssetIDs     []string                          `json:"evidence_asset_ids"`
}

type CreateVariantManifestDraftInput struct {
	Identity json.RawMessage                `json:"identity"`
	Regions  []VariantDifferenceRegionInput `json:"regions"`
	ActorID  uint                           `json:"-"`
}

type UpdateVariantManifestDraftInput struct {
	Identity json.RawMessage                `json:"identity"`
	Regions  []VariantDifferenceRegionInput `json:"regions"`
	ActorID  uint                           `json:"-"`
}

type VariantManifestValidationIssue struct {
	Code    string `json:"code"`
	Path    string `json:"path"`
	Message string `json:"message"`
}

type VariantManifestValidationError struct {
	Issues []VariantManifestValidationIssue
}

func (e *VariantManifestValidationError) Error() string { return ErrVariantManifestValidation.Error() }
func (e *VariantManifestValidationError) Unwrap() error { return ErrVariantManifestValidation }

type VariantManifestService struct{ db *gorm.DB }

func NewVariantManifestService(db *gorm.DB) *VariantManifestService {
	return &VariantManifestService{db: db}
}

func (s *VariantManifestService) CreateDraft(ctx context.Context, skuPublicID string, input CreateVariantManifestDraftInput) (*models.VariantIdentityManifestVersion, error) {
	var created models.VariantIdentityManifestVersion
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		membership, family, sku, err := activeManifestMembership(tx, skuPublicID)
		if err != nil {
			return err
		}
		_ = membership
		identity, regions, err := normalizeManifestInput(input.Identity, input.Regions, family, sku.ID, tx)
		if err != nil {
			return err
		}
		var manifest models.VariantIdentityManifest
		err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("sk_uid = ?", sku.ID).First(&manifest).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			manifest = models.VariantIdentityManifest{PublicID: uuid.NewString(), ModelFamilyID: family.ID, SKUID: sku.ID, CreatedByID: input.ActorID}
			if err := tx.Create(&manifest).Error; err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else if manifest.ModelFamilyID != family.ID {
			return ErrVariantManifestInvalid
		}
		var draftCount int64
		if err := tx.Model(&models.VariantIdentityManifestVersion{}).Where("variant_identity_manifest_id = ? AND status = ?", manifest.ID, models.VariantManifestDraft).Count(&draftCount).Error; err != nil {
			return err
		}
		if draftCount != 0 {
			return ErrVariantManifestDraftExists
		}
		var maxVersion int
		if err := tx.Model(&models.VariantIdentityManifestVersion{}).Where("variant_identity_manifest_id = ?", manifest.ID).Select("COALESCE(MAX(version_number), 0)").Scan(&maxVersion).Error; err != nil {
			return err
		}
		created = models.VariantIdentityManifestVersion{PublicID: uuid.NewString(), VariantIdentityManifestID: manifest.ID, VersionNumber: maxVersion + 1, Status: models.VariantManifestDraft, IdentityJSON: identity, CreatedByID: input.ActorID}
		if err := tx.Create(&created).Error; err != nil {
			return mapVariantManifestCreateError(err)
		}
		if err := createVariantRegions(tx, created.ID, regions); err != nil {
			return err
		}
		if err := writeVariantManifestAudit(tx, input.ActorID, "variant_identity_manifest.draft_created", manifest.PublicID, map[string]string{"action": "draft_create", "manifest_id": manifest.PublicID, "version_id": created.PublicID, "sku_id": sku.PublicID}); err != nil {
			return err
		}
		return tx.Preload("Regions.EvidenceAssets").First(&created, created.ID).Error
	})
	if err != nil {
		return nil, err
	}
	return &created, nil
}

func (s *VariantManifestService) UpdateDraft(ctx context.Context, versionPublicID string, input UpdateVariantManifestDraftInput) (*models.VariantIdentityManifestVersion, error) {
	var updated models.VariantIdentityManifestVersion
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var locked models.VariantIdentityManifestVersion
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").Where("public_id = ?", versionPublicID).First(&locked).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrVariantManifestVersionNotFound
			}
			return err
		}
		version, manifest, family, sku, err := manifestVersionContext(tx, versionPublicID)
		if err != nil {
			return err
		}
		if version.Status != models.VariantManifestDraft {
			return ErrVariantManifestImmutable
		}
		identity, regions, err := normalizeManifestInput(input.Identity, input.Regions, family, sku.ID, tx)
		if err != nil {
			return err
		}
		// The lock handles MySQL and the status predicate is the final guard for
		// any database where FOR UPDATE is weaker or unavailable in tests.
		result := tx.Model(&models.VariantIdentityManifestVersion{}).Where("id = ? AND status = ?", version.ID, models.VariantManifestDraft).Update("identity_json", identity)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrVariantManifestImmutable
		}
		if err := tx.Where("variant_difference_region_id IN (?)", tx.Model(&models.VariantDifferenceRegion{}).Select("id").Where("variant_identity_manifest_version_id = ?", version.ID)).Delete(&models.VariantDifferenceRegionEvidenceAsset{}).Error; err != nil {
			return err
		}
		if err := tx.Where("variant_identity_manifest_version_id = ?", version.ID).Delete(&models.VariantDifferenceRegion{}).Error; err != nil {
			return err
		}
		if err := createVariantRegions(tx, version.ID, regions); err != nil {
			return err
		}
		if err := writeVariantManifestAudit(tx, input.ActorID, "variant_identity_manifest.draft_updated", manifest.PublicID, map[string]string{"action": "draft_update", "manifest_id": manifest.PublicID, "version_id": version.PublicID, "sku_id": sku.PublicID}); err != nil {
			return err
		}
		if err := tx.Preload("Regions.EvidenceAssets").First(&updated, version.ID).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &updated, nil
}

func (s *VariantManifestService) CopyVersion(ctx context.Context, skuPublicID, sourceVersionPublicID string, actorID uint) (*models.VariantIdentityManifestVersion, error) {
	var copied models.VariantIdentityManifestVersion
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		_, family, sku, err := activeManifestMembership(tx, skuPublicID)
		if err != nil {
			return err
		}
		var manifest models.VariantIdentityManifest
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("sk_uid = ? AND model_family_id = ?", sku.ID, family.ID).First(&manifest).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrVariantManifestNotFound
			}
			return err
		}
		var source models.VariantIdentityManifestVersion
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Preload("Regions.EvidenceAssets").Where("public_id = ? AND variant_identity_manifest_id = ?", sourceVersionPublicID, manifest.ID).First(&source).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrVariantManifestVersionNotFound
			}
			return err
		}
		if source.Status != models.VariantManifestPublished {
			return ErrVariantManifestSourceNotPublished
		}
		var drafts int64
		if err := tx.Model(&models.VariantIdentityManifestVersion{}).Where("variant_identity_manifest_id = ? AND status = ?", manifest.ID, models.VariantManifestDraft).Count(&drafts).Error; err != nil {
			return err
		}
		if drafts != 0 {
			return ErrVariantManifestDraftExists
		}
		var maxVersion int
		if err := tx.Model(&models.VariantIdentityManifestVersion{}).Where("variant_identity_manifest_id = ?", manifest.ID).Select("COALESCE(MAX(version_number), 0)").Scan(&maxVersion).Error; err != nil {
			return err
		}
		copied = models.VariantIdentityManifestVersion{PublicID: uuid.NewString(), VariantIdentityManifestID: manifest.ID, VersionNumber: maxVersion + 1, Status: models.VariantManifestDraft, IdentityJSON: append(json.RawMessage(nil), source.IdentityJSON...), CreatedByID: actorID}
		if err := tx.Create(&copied).Error; err != nil {
			return mapVariantManifestCreateError(err)
		}
		for _, region := range source.Regions {
			clone := models.VariantDifferenceRegion{PublicID: uuid.NewString(), VariantIdentityManifestVersionID: copied.ID, Key: region.Key, DifferenceKind: region.DifferenceKind, Strictness: region.Strictness, DescriptionZH: region.DescriptionZH, DescriptionEN: region.DescriptionEN, ShapeJSON: append(json.RawMessage(nil), region.ShapeJSON...), ForbiddenInheritanceJSON: append(json.RawMessage(nil), region.ForbiddenInheritanceJSON...), RequiredViewKeysJSON: append(json.RawMessage(nil), region.RequiredViewKeysJSON...)}
			if err := tx.Create(&clone).Error; err != nil {
				return err
			}
			for _, evidence := range region.EvidenceAssets {
				if err := tx.Create(&models.VariantDifferenceRegionEvidenceAsset{VariantDifferenceRegionID: clone.ID, AssetID: evidence.AssetID}).Error; err != nil {
					return err
				}
			}
		}
		if err := writeVariantManifestAudit(tx, actorID, "variant_identity_manifest.version_copied", manifest.PublicID, map[string]string{"action": "copy", "manifest_id": manifest.PublicID, "version_id": copied.PublicID, "source_version_id": source.PublicID, "sku_id": sku.PublicID}); err != nil {
			return err
		}
		return tx.Preload("Regions.EvidenceAssets").First(&copied, copied.ID).Error
	})
	if err != nil {
		return nil, err
	}
	return &copied, nil
}

func (s *VariantManifestService) Validate(ctx context.Context, versionPublicID string) ([]VariantManifestValidationIssue, error) {
	var issues []VariantManifestValidationIssue
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		version, _, family, sku, err := manifestVersionContext(tx, versionPublicID)
		if err != nil {
			return err
		}
		issues = validateManifestVersion(version, family, sku.ID, tx)
		return nil
	})
	return issues, err
}

func (s *VariantManifestService) Publish(ctx context.Context, versionPublicID string, actorID uint) (*models.VariantIdentityManifestVersion, error) {
	var published models.VariantIdentityManifestVersion
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var locked models.VariantIdentityManifestVersion
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").Where("public_id = ?", versionPublicID).First(&locked).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrVariantManifestVersionNotFound
			}
			return err
		}
		version, manifest, family, sku, err := manifestVersionContext(tx, versionPublicID)
		if err != nil {
			return err
		}
		if version.Status != models.VariantManifestDraft {
			return ErrVariantManifestImmutable
		}
		if issues := validateManifestVersion(version, family, sku.ID, tx); len(issues) > 0 {
			return &VariantManifestValidationError{Issues: issues}
		}
		now := time.Now().UTC()
		result := tx.Model(&models.VariantIdentityManifestVersion{}).Where("id = ? AND status = ?", version.ID, models.VariantManifestDraft).Updates(map[string]any{"status": models.VariantManifestPublished, "draft_guard": nil, "published_by_id": actorID, "published_at": now})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrVariantManifestImmutable
		}
		if err := writeVariantManifestAudit(tx, actorID, "variant_identity_manifest.published", manifest.PublicID, map[string]string{"action": "publish", "manifest_id": manifest.PublicID, "version_id": version.PublicID, "sku_id": sku.PublicID}); err != nil {
			return err
		}
		return tx.Preload("Regions.EvidenceAssets").First(&published, version.ID).Error
	})
	if err != nil {
		return nil, err
	}
	return &published, nil
}

// GetForSKU returns only the latest published version. Draft identity data is
// intentionally not a read contract for photographers or viewers.
func (s *VariantManifestService) GetForSKU(ctx context.Context, skuPublicID string) (*models.VariantIdentityManifestVersion, error) {
	var sku models.SKU
	if err := s.db.WithContext(ctx).Where("public_id = ?", skuPublicID).First(&sku).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSKUNotFound
		}
		return nil, err
	}
	var version models.VariantIdentityManifestVersion
	err := s.db.WithContext(ctx).Joins("JOIN variant_identity_manifests ON variant_identity_manifests.id = variant_identity_manifest_versions.variant_identity_manifest_id").Where("variant_identity_manifests.sk_uid = ? AND variant_identity_manifest_versions.status = ?", sku.ID, models.VariantManifestPublished).Order("variant_identity_manifest_versions.version_number DESC").Preload("Regions.EvidenceAssets").First(&version).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrVariantManifestNotFound
	}
	if err != nil {
		return nil, err
	}
	return &version, nil
}

func activeManifestMembership(tx *gorm.DB, skuPublicID string) (*models.ModelFamilyMember, *models.ModelFamily, *models.SKU, error) {
	var sku models.SKU
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("public_id = ?", skuPublicID).First(&sku).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, nil, ErrSKUNotFound
		}
		return nil, nil, nil, err
	}
	var member models.ModelFamilyMember
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("sk_uid = ? AND removed_at IS NULL", sku.ID).First(&member).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, nil, ErrVariantManifestInvalid
		}
		return nil, nil, nil, err
	}
	var family models.ModelFamily
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&family, member.ModelFamilyID).Error; err != nil {
		return nil, nil, nil, err
	}
	if family.Status != models.ModelFamilyActive {
		return nil, nil, nil, ErrModelFamilyArchived
	}
	return &member, &family, &sku, nil
}

func manifestVersionContext(tx *gorm.DB, versionPublicID string) (*models.VariantIdentityManifestVersion, *models.VariantIdentityManifest, *models.ModelFamily, *models.SKU, error) {
	db := tx.Session(&gorm.Session{NewDB: true})
	var version models.VariantIdentityManifestVersion
	if err := db.Preload("Regions.EvidenceAssets").Where("public_id = ?", versionPublicID).First(&version).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, nil, nil, ErrVariantManifestVersionNotFound
		}
		return nil, nil, nil, nil, err
	}
	var manifest models.VariantIdentityManifest
	if err := db.First(&manifest, version.VariantIdentityManifestID).Error; err != nil {
		return nil, nil, nil, nil, err
	}
	var sku models.SKU
	if err := db.First(&sku, manifest.SKUID).Error; err != nil {
		return nil, nil, nil, nil, err
	}
	var family models.ModelFamily
	if err := db.First(&family, manifest.ModelFamilyID).Error; err != nil {
		return nil, nil, nil, nil, err
	}
	if family.Status != models.ModelFamilyActive {
		return nil, nil, nil, nil, ErrModelFamilyArchived
	}
	var member models.ModelFamilyMember
	if err := db.Where("model_family_id = ? AND sk_uid = ? AND removed_at IS NULL", family.ID, sku.ID).First(&member).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, nil, nil, ErrVariantManifestInvalid
		}
		return nil, nil, nil, nil, err
	}
	return &version, &manifest, &family, &sku, nil
}

func normalizeManifestInput(identityRaw json.RawMessage, regionInputs []VariantDifferenceRegionInput, family *models.ModelFamily, skuID uint, tx *gorm.DB) (json.RawMessage, []VariantDifferenceRegionInput, error) {
	identity, err := normalizeVariantIdentity(identityRaw, family)
	if err != nil {
		return nil, nil, err
	}
	regions, err := normalizeVariantRegions(regionInputs, family, skuID, tx)
	if err != nil {
		return nil, nil, err
	}
	return identity, regions, nil
}

func normalizeVariantIdentity(raw json.RawMessage, family *models.ModelFamily) (json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, ErrVariantManifestInvalid
	}
	for _, key := range []string{"schema", "colors", "material", "finish", "texture", "labels", "ports", "controls", "accessories", "packaging", "other", "must_prove_with_target_assets"} {
		if _, found := fields[key]; !found {
			return nil, ErrVariantManifestInvalid
		}
	}
	var document VariantIdentityDocumentV1
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil {
		return nil, ErrVariantManifestInvalid
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, ErrVariantManifestInvalid
	}
	if document.Schema != variantIdentitySchemaV1 {
		return nil, ErrVariantManifestInvalid
	}
	var dimensions []string
	if err := json.Unmarshal(family.VariationDimensionsJSON, &dimensions); err != nil {
		return nil, ErrVariantManifestInvalid
	}
	allowed := make(map[string]bool, len(dimensions))
	for _, value := range dimensions {
		allowed[value] = true
	}
	if len(document.Colors) > 0 && !allowed["color"] || document.Material != "" && !allowed["material"] || document.Finish != "" && !allowed["finish"] || document.Texture != "" && !allowed["texture"] || len(document.Labels) > 0 && !allowed["labels"] || len(document.Ports) > 0 && !allowed["ports"] || len(document.Controls) > 0 && !allowed["controls"] || len(document.Accessories) > 0 && !allowed["accessories"] || len(document.Packaging) > 0 && !allowed["packaging"] || len(document.Other) > 0 && !allowed["other"] {
		return nil, ErrVariantManifestInvalid
	}
	keys := map[string]struct{}{}
	for i := range document.Colors {
		if !normalizeColor(&document.Colors[i], keys) {
			return nil, ErrVariantManifestInvalid
		}
	}
	for i := range document.Labels {
		if !normalizeLabel(&document.Labels[i], keys) {
			return nil, ErrVariantManifestInvalid
		}
	}
	for i := range document.Ports {
		if !normalizeFeature(&document.Ports[i], keys) {
			return nil, ErrVariantManifestInvalid
		}
	}
	for i := range document.Controls {
		if !normalizeFeature(&document.Controls[i], keys) {
			return nil, ErrVariantManifestInvalid
		}
	}
	for i := range document.Packaging {
		if !normalizeFeature(&document.Packaging[i], keys) {
			return nil, ErrVariantManifestInvalid
		}
	}
	for i := range document.Other {
		if !normalizeFeature(&document.Other[i], keys) {
			return nil, ErrVariantManifestInvalid
		}
	}
	for _, text := range []*string{&document.Material, &document.Finish, &document.Texture} {
		if !normalizeSafeText(text, 180, false) {
			return nil, ErrVariantManifestInvalid
		}
	}
	for i := range document.Accessories {
		if !normalizeSafeText(&document.Accessories[i], 180, true) {
			return nil, ErrVariantManifestInvalid
		}
	}
	if !uniqueSafeKeys(document.MustProveWithTargetAssets, 80) {
		return nil, ErrVariantManifestInvalid
	}
	document.Colors = normalizedColorRegions(document.Colors)
	document.Labels = normalizedLabels(document.Labels)
	document.Ports = normalizedFeatures(document.Ports)
	document.Controls = normalizedFeatures(document.Controls)
	document.Accessories = normalizedStrings(document.Accessories)
	document.Packaging = normalizedFeatures(document.Packaging)
	document.Other = normalizedFeatures(document.Other)
	document.MustProveWithTargetAssets = normalizedStrings(document.MustProveWithTargetAssets)
	encoded, err := json.Marshal(document)
	if err != nil {
		return nil, err
	}
	return encoded, nil
}

func normalizeColor(value *VariantColorRegion, seen map[string]struct{}) bool {
	return normalizeKey(&value.Key, seen) && normalizeSafeText(&value.Name, 120, true) && (value.Value == "" || hexColor.MatchString(value.Value))
}
func normalizeLabel(value *VariantLabel, seen map[string]struct{}) bool {
	return normalizeKey(&value.Key, seen) && normalizeSafeText(&value.Text, 240, true) && normalizeOptionalKey(&value.RegionKey)
}
func normalizeFeature(value *VariantFeature, seen map[string]struct{}) bool {
	return normalizeKey(&value.Key, seen) && normalizeSafeText(&value.Description, 240, true) && normalizeOptionalKey(&value.RegionKey)
}
func normalizeKey(value *string, seen map[string]struct{}) bool {
	if !normalizeSafeText(value, 80, true) {
		return false
	}
	if _, found := seen[*value]; found {
		return false
	}
	seen[*value] = struct{}{}
	return true
}
func normalizeOptionalKey(value *string) bool {
	if *value == "" {
		return true
	}
	return normalizeSafeText(value, 80, true)
}
func uniqueSafeKeys(values []string, max int) bool {
	seen := map[string]struct{}{}
	for i := range values {
		if !normalizeSafeText(&values[i], max, true) {
			return false
		}
		if _, found := seen[values[i]]; found {
			return false
		}
		seen[values[i]] = struct{}{}
	}
	return true
}
func normalizedStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
func normalizedColorRegions(values []VariantColorRegion) []VariantColorRegion {
	if values == nil {
		return []VariantColorRegion{}
	}
	return values
}
func normalizedLabels(values []VariantLabel) []VariantLabel {
	if values == nil {
		return []VariantLabel{}
	}
	return values
}
func normalizedFeatures(values []VariantFeature) []VariantFeature {
	if values == nil {
		return []VariantFeature{}
	}
	return values
}
func normalizeSafeText(value *string, max int, required bool) bool {
	*value = strings.TrimSpace(*value)
	if (required && *value == "") || len(*value) > max || containsUnsafeReference(*value) {
		return false
	}
	return true
}
func containsUnsafeReference(value string) bool {
	lower := strings.ToLower(value)
	return strings.Contains(lower, "://") || strings.Contains(lower, "s3:") || strings.Contains(lower, "minio") || strings.Contains(lower, "object_key") || strings.Contains(lower, "assets/final/") || strings.Contains(lower, "@")
}

func normalizeVariantRegions(inputs []VariantDifferenceRegionInput, family *models.ModelFamily, skuID uint, tx *gorm.DB) ([]VariantDifferenceRegionInput, error) {
	var dimensions []string
	if err := json.Unmarshal(family.VariationDimensionsJSON, &dimensions); err != nil {
		return nil, ErrVariantManifestInvalid
	}
	allowed := map[string]bool{}
	for _, value := range dimensions {
		allowed[value] = true
	}
	seen := map[string]struct{}{}
	for index := range inputs {
		region := &inputs[index]
		if !normalizeKey(&region.Key, seen) || !allowed[string(region.DifferenceKind)] || !validStrictness(region.Strictness) || !normalizeSafeText(&region.DescriptionZH, 1000, false) || !normalizeSafeText(&region.DescriptionEN, 1000, false) {
			return nil, ErrVariantManifestInvalid
		}
		shape, err := normalizeRegionShape(region.Shape)
		if err != nil {
			return nil, err
		}
		region.Shape = shape
		if !uniqueSafeKeys(region.ForbiddenInheritance, 80) || !uniqueSafeKeys(region.RequiredViewKeys, 80) {
			return nil, ErrVariantManifestInvalid
		}
		region.ForbiddenInheritance = normalizedStrings(region.ForbiddenInheritance)
		region.RequiredViewKeys = normalizedStrings(region.RequiredViewKeys)
		if !uniqueCanonicalUUIDs(region.EvidenceAssetIDs) {
			return nil, ErrVariantManifestInvalid
		}
		for _, assetID := range region.EvidenceAssetIDs {
			if err := validateEvidenceAsset(tx, assetID, skuID); err != nil {
				return nil, err
			}
		}
	}
	return inputs, nil
}

func validStrictness(value models.DifferenceRegionStrictness) bool {
	return value == models.DifferenceRegionExact || value == models.DifferenceRegionPreserve || value == models.DifferenceRegionDescriptive
}
func uniqueCanonicalUUIDs(values []string) bool {
	seen := map[string]struct{}{}
	for _, value := range values {
		parsed, err := uuid.Parse(value)
		if err != nil || parsed == uuid.Nil || parsed.String() != value {
			return false
		}
		if _, found := seen[value]; found {
			return false
		}
		seen[value] = struct{}{}
	}
	return true
}

func normalizeRegionShape(raw json.RawMessage) (json.RawMessage, error) {
	var shape struct {
		Kind   string      `json:"kind"`
		X      *float64    `json:"x"`
		Y      *float64    `json:"y"`
		Width  *float64    `json:"width"`
		Height *float64    `json:"height"`
		Points [][]float64 `json:"points,omitempty"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&shape); err != nil {
		return nil, ErrVariantManifestInvalid
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, ErrVariantManifestInvalid
	}
	switch shape.Kind {
	case "rectangle":
		if shape.X == nil || shape.Y == nil || shape.Width == nil || shape.Height == nil || !unit(*shape.X) || !unit(*shape.Y) || *shape.Width <= 0 || *shape.Height <= 0 || *shape.X+*shape.Width > 1 || *shape.Y+*shape.Height > 1 {
			return nil, ErrVariantManifestInvalid
		}
		shape.Points = nil
	case "polygon":
		if shape.X != nil || shape.Y != nil || shape.Width != nil || shape.Height != nil || len(shape.Points) < 3 {
			return nil, ErrVariantManifestInvalid
		}
		for _, point := range shape.Points {
			if len(point) != 2 || !unit(point[0]) || !unit(point[1]) {
				return nil, ErrVariantManifestInvalid
			}
		}
	default:
		return nil, ErrVariantManifestInvalid
	}
	encoded, err := json.Marshal(shape)
	if err != nil {
		return nil, err
	}
	return encoded, nil
}
func unit(value float64) bool { return value >= 0 && value <= 1 }

func createVariantRegions(tx *gorm.DB, versionID uint, regions []VariantDifferenceRegionInput) error {
	for _, input := range regions {
		region := models.VariantDifferenceRegion{PublicID: uuid.NewString(), VariantIdentityManifestVersionID: versionID, Key: input.Key, DifferenceKind: input.DifferenceKind, Strictness: input.Strictness, DescriptionZH: input.DescriptionZH, DescriptionEN: input.DescriptionEN, ShapeJSON: input.Shape, ForbiddenInheritanceJSON: mustJSON(input.ForbiddenInheritance), RequiredViewKeysJSON: mustJSON(input.RequiredViewKeys)}
		if err := tx.Create(&region).Error; err != nil {
			return err
		}
		for _, publicID := range input.EvidenceAssetIDs {
			var asset models.Asset
			if err := tx.Select("id").Where("public_id = ?", publicID).First(&asset).Error; err != nil {
				return err
			}
			if err := tx.Create(&models.VariantDifferenceRegionEvidenceAsset{VariantDifferenceRegionID: region.ID, AssetID: asset.ID}).Error; err != nil {
				return err
			}
		}
	}
	return nil
}
func mustJSON(value []string) json.RawMessage {
	encoded, _ := json.Marshal(normalizedStrings(value))
	return encoded
}
func validateEvidenceAsset(tx *gorm.DB, publicID string, skuID uint) error {
	var asset models.Asset
	if err := tx.Where("public_id = ?", publicID).First(&asset).Error; err != nil {
		return ErrVariantManifestInvalid
	}
	if asset.SKUID != skuID || asset.ReviewStatus != "approved" || !strings.HasPrefix(asset.MIMEType, "image/") || asset.Width <= 0 || asset.Height <= 0 || asset.ByteCount <= 0 || !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(asset.SHA256) {
		return ErrVariantManifestInvalid
	}
	return nil
}

func validateManifestVersion(version *models.VariantIdentityManifestVersion, family *models.ModelFamily, skuID uint, tx *gorm.DB) []VariantManifestValidationIssue {
	issues := make([]VariantManifestValidationIssue, 0)
	if _, err := normalizeVariantIdentity(version.IdentityJSON, family); err != nil {
		issues = append(issues, VariantManifestValidationIssue{Code: "identity_invalid", Path: "identity", Message: "identity document is invalid"})
	}
	for _, region := range version.Regions {
		if region.Strictness != models.DifferenceRegionExact {
			continue
		}
		var required []string
		_ = json.Unmarshal(region.RequiredViewKeysJSON, &required)
		if len(required) == 0 {
			issues = append(issues, VariantManifestValidationIssue{Code: "exact_region_view_required", Path: "regions." + region.Key + ".required_view_keys", Message: "exact regions require at least one capture view"})
		}
		if len(region.EvidenceAssets) == 0 {
			issues = append(issues, VariantManifestValidationIssue{Code: "exact_region_evidence_required", Path: "regions." + region.Key + ".evidence_asset_ids", Message: "exact regions require approved target evidence"})
			continue
		}
		for _, evidence := range region.EvidenceAssets {
			var asset models.Asset
			if err := tx.First(&asset, evidence.AssetID).Error; err != nil || validateAssetRecord(asset, skuID) != nil {
				issues = append(issues, VariantManifestValidationIssue{Code: "exact_region_evidence_invalid", Path: "regions." + region.Key + ".evidence_asset_ids", Message: "exact region evidence must be an approved target asset"})
				break
			}
		}
	}
	sort.Slice(issues, func(i, j int) bool {
		if issues[i].Path == issues[j].Path {
			return issues[i].Code < issues[j].Code
		}
		return issues[i].Path < issues[j].Path
	})
	return issues
}
func validateAssetRecord(asset models.Asset, skuID uint) error {
	if asset.SKUID != skuID || asset.ReviewStatus != "approved" || !strings.HasPrefix(asset.MIMEType, "image/") || asset.Width <= 0 || asset.Height <= 0 || asset.ByteCount <= 0 || !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(asset.SHA256) {
		return ErrVariantManifestInvalid
	}
	return nil
}
func mapVariantManifestCreateError(err error) error {
	if isUniqueViolation(err) {
		return ErrVariantManifestDraftExists
	}
	return err
}
func writeVariantManifestAudit(tx *gorm.DB, actorID uint, eventType, entityPublicID string, metadata map[string]string) error {
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	actor := actorID
	return tx.Create(&models.AIAuditEvent{PublicID: uuid.NewString(), EventType: eventType, EntityType: "variant_identity_manifest", EntityPublicID: entityPublicID, ActorID: &actor, MetadataJSON: encoded}).Error
}
