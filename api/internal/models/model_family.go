package models

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func ensurePublicID(value *string) error {
	if *value == "" {
		*value = uuid.NewString()
		return nil
	}
	parsed, err := uuid.Parse(*value)
	if err != nil || parsed == uuid.Nil {
		return fmt.Errorf("public ID must be a UUID")
	}
	*value = parsed.String()
	return nil
}

type ModelFamilyStatus string

const (
	ModelFamilyActive   ModelFamilyStatus = "active"
	ModelFamilyArchived ModelFamilyStatus = "archived"
)

type VariantManifestStatus string

const (
	VariantManifestDraft     VariantManifestStatus = "draft"
	VariantManifestPublished VariantManifestStatus = "published"
	VariantManifestArchived  VariantManifestStatus = "archived"
)

type DifferenceKind string

const (
	DifferenceKindColor       DifferenceKind = "color"
	DifferenceKindMaterial    DifferenceKind = "material"
	DifferenceKindFinish      DifferenceKind = "finish"
	DifferenceKindTexture     DifferenceKind = "texture"
	DifferenceKindTrim        DifferenceKind = "trim"
	DifferenceKindPorts       DifferenceKind = "ports"
	DifferenceKindControls    DifferenceKind = "controls"
	DifferenceKindLabels      DifferenceKind = "labels"
	DifferenceKindAccessories DifferenceKind = "accessories"
	DifferenceKindPackaging   DifferenceKind = "packaging"
	DifferenceKindOther       DifferenceKind = "other"
)

type DifferenceRegionStrictness string

const (
	DifferenceRegionExact       DifferenceRegionStrictness = "exact"
	DifferenceRegionPreserve    DifferenceRegionStrictness = "preserve"
	DifferenceRegionDescriptive DifferenceRegionStrictness = "descriptive"
)

type ModelFamily struct {
	ID                      uint                `gorm:"primaryKey" json:"-"`
	PublicID                string              `gorm:"size:36;uniqueIndex;not null" json:"public_id"`
	Brand                   string              `gorm:"size:120;index;not null" json:"brand"`
	NameZH                  string              `gorm:"size:180;not null" json:"name_zh"`
	NameEN                  string              `gorm:"size:180;not null" json:"name_en"`
	ModelCode               string              `gorm:"size:120;uniqueIndex;not null" json:"model_code"`
	CommonStructureJSON     json.RawMessage     `gorm:"type:json;not null" json:"common_structure"`
	VariationDimensionsJSON json.RawMessage     `gorm:"type:json;not null" json:"variation_dimensions"`
	Status                  ModelFamilyStatus   `gorm:"size:32;index;not null;default:active" json:"status"`
	CreatedByID             uint                `gorm:"index;not null" json:"-"`
	CreatedAt               time.Time           `json:"created_at"`
	UpdatedAt               time.Time           `json:"updated_at"`
	Members                 []ModelFamilyMember `json:"members,omitempty"`
}

func (family *ModelFamily) BeforeCreate(*gorm.DB) error {
	if err := ensurePublicID(&family.PublicID); err != nil {
		return err
	}
	if len(family.CommonStructureJSON) == 0 {
		family.CommonStructureJSON = []byte(`{}`)
	}
	if len(family.VariationDimensionsJSON) == 0 {
		family.VariationDimensionsJSON = []byte(`[]`)
	}
	if family.Status == "" {
		family.Status = ModelFamilyActive
	}
	return nil
}

type ModelFamilyMember struct {
	ID            uint       `gorm:"primaryKey" json:"-"`
	PublicID      string     `gorm:"size:36;uniqueIndex;not null" json:"public_id"`
	ModelFamilyID uint       `gorm:"index;not null" json:"-"`
	SKUID         uint       `gorm:"uniqueIndex:idx_active_family_member,priority:1;index;not null" json:"-"`
	ActiveGuard   *string    `gorm:"size:16;uniqueIndex:idx_active_family_member,priority:2;check:chk_model_family_member_active_guard,(removed_at IS NULL AND active_guard = 'active') OR (removed_at IS NOT NULL AND active_guard IS NULL)" json:"-"`
	AddedByID     uint       `gorm:"index;not null" json:"-"`
	RemovedByID   *uint      `gorm:"index" json:"-"`
	RemovedAt     *time.Time `json:"removed_at"`
	CreatedAt     time.Time  `json:"created_at"`
}

func (member *ModelFamilyMember) BeforeCreate(*gorm.DB) error {
	if err := ensurePublicID(&member.PublicID); err != nil {
		return err
	}
	if member.ActiveGuard == nil && member.RemovedAt == nil {
		active := "active"
		member.ActiveGuard = &active
	}
	return nil
}

type VariantIdentityManifest struct {
	ID            uint                             `gorm:"primaryKey" json:"-"`
	PublicID      string                           `gorm:"size:36;uniqueIndex;not null" json:"public_id"`
	ModelFamilyID uint                             `gorm:"uniqueIndex:idx_variant_manifest_family_sku,priority:1;index;not null" json:"-"`
	SKUID         uint                             `gorm:"uniqueIndex:idx_variant_manifest_family_sku,priority:2;index;not null" json:"-"`
	CreatedByID   uint                             `gorm:"index;not null" json:"-"`
	CreatedAt     time.Time                        `json:"created_at"`
	UpdatedAt     time.Time                        `json:"updated_at"`
	Versions      []VariantIdentityManifestVersion `json:"versions,omitempty"`
}

func (manifest *VariantIdentityManifest) BeforeCreate(*gorm.DB) error {
	return ensurePublicID(&manifest.PublicID)
}

type VariantIdentityManifestVersion struct {
	ID                        uint                      `gorm:"primaryKey" json:"-"`
	PublicID                  string                    `gorm:"size:36;uniqueIndex;not null" json:"public_id"`
	VariantIdentityManifestID uint                      `gorm:"uniqueIndex:idx_variant_manifest_version,priority:1;uniqueIndex:idx_variant_manifest_draft,priority:1;not null" json:"-"`
	VersionNumber             int                       `gorm:"uniqueIndex:idx_variant_manifest_version,priority:2;check:chk_variant_manifest_version,version_number > 0;not null" json:"version_number"`
	Status                    VariantManifestStatus     `gorm:"size:32;index;not null;default:draft" json:"status"`
	DraftGuard                *string                   `gorm:"size:16;uniqueIndex:idx_variant_manifest_draft,priority:2;check:chk_variant_manifest_draft_guard,(status = 'draft' AND draft_guard = 'draft') OR (status <> 'draft' AND draft_guard IS NULL)" json:"-"`
	IdentityJSON              json.RawMessage           `gorm:"type:json;not null" json:"identity"`
	CreatedByID               uint                      `gorm:"index;not null" json:"-"`
	PublishedByID             *uint                     `gorm:"index" json:"-"`
	PublishedAt               *time.Time                `json:"published_at"`
	CreatedAt                 time.Time                 `json:"created_at"`
	Regions                   []VariantDifferenceRegion `json:"regions,omitempty"`
}

func (version *VariantIdentityManifestVersion) BeforeCreate(*gorm.DB) error {
	if err := ensurePublicID(&version.PublicID); err != nil {
		return err
	}
	if len(version.IdentityJSON) == 0 {
		version.IdentityJSON = []byte(`{}`)
	}
	if version.Status == "" {
		version.Status = VariantManifestDraft
	}
	if version.DraftGuard == nil && version.Status == VariantManifestDraft {
		draft := "draft"
		version.DraftGuard = &draft
	}
	return nil
}

type VariantDifferenceRegion struct {
	ID                               uint                                   `gorm:"primaryKey" json:"-"`
	PublicID                         string                                 `gorm:"size:36;uniqueIndex;not null" json:"public_id"`
	VariantIdentityManifestVersionID uint                                   `gorm:"uniqueIndex:idx_variant_difference_region_key,priority:1;index;not null" json:"-"`
	Key                              string                                 `gorm:"size:80;uniqueIndex:idx_variant_difference_region_key,priority:2;not null" json:"key"`
	DifferenceKind                   DifferenceKind                         `gorm:"size:32;index;not null" json:"difference_kind"`
	Strictness                       DifferenceRegionStrictness             `gorm:"size:32;index;not null" json:"strictness"`
	DescriptionZH                    string                                 `gorm:"type:text" json:"description_zh"`
	DescriptionEN                    string                                 `gorm:"type:text" json:"description_en"`
	ShapeJSON                        json.RawMessage                        `gorm:"type:json;not null" json:"shape"`
	ForbiddenInheritanceJSON         json.RawMessage                        `gorm:"type:json;not null" json:"forbidden_inheritance"`
	RequiredViewKeysJSON             json.RawMessage                        `gorm:"type:json;not null" json:"required_view_keys"`
	CreatedAt                        time.Time                              `json:"created_at"`
	UpdatedAt                        time.Time                              `json:"updated_at"`
	EvidenceAssets                   []VariantDifferenceRegionEvidenceAsset `json:"evidence_assets,omitempty"`
}

func (region *VariantDifferenceRegion) BeforeCreate(*gorm.DB) error {
	if err := ensurePublicID(&region.PublicID); err != nil {
		return err
	}
	if len(region.ShapeJSON) == 0 {
		region.ShapeJSON = []byte(`{}`)
	}
	if len(region.ForbiddenInheritanceJSON) == 0 {
		region.ForbiddenInheritanceJSON = []byte(`[]`)
	}
	if len(region.RequiredViewKeysJSON) == 0 {
		region.RequiredViewKeysJSON = []byte(`[]`)
	}
	return nil
}

type VariantDifferenceRegionEvidenceAsset struct {
	ID                        uint      `gorm:"primaryKey" json:"-"`
	VariantDifferenceRegionID uint      `gorm:"uniqueIndex:idx_variant_difference_region_evidence,priority:1;index;not null" json:"-"`
	AssetID                   uint      `gorm:"uniqueIndex:idx_variant_difference_region_evidence,priority:2;index;not null" json:"-"`
	CreatedAt                 time.Time `json:"created_at"`
}

// StyleReferenceGrant permits only non-product visual treatment to cross SKU
// boundaries. The derivative is mandatory so the source product is never sent
// to a later task as a style reference.
type StyleReferenceGrant struct {
	ID                     uint      `gorm:"primaryKey" json:"-"`
	PublicID               string    `gorm:"size:36;uniqueIndex;not null" json:"public_id"`
	AssetID                uint      `gorm:"uniqueIndex:idx_style_reference_version,priority:1;index;not null" json:"-"`
	Version                int       `gorm:"uniqueIndex:idx_style_reference_version,priority:2;not null;default:1" json:"version"`
	DescriptionZH          string    `gorm:"type:text;not null" json:"description_zh"`
	DescriptionEN          string    `gorm:"type:text;not null" json:"description_en"`
	ExclusionMaskObjectKey string    `gorm:"size:768;not null" json:"-"`
	DerivativeObjectKey    string    `gorm:"size:768;not null" json:"-"`
	DerivativeSHA256       string    `gorm:"size:64;index;not null" json:"derivative_sha256"`
	Status                 string    `gorm:"size:32;index;not null;default:approved" json:"status"`
	ReviewedByID           uint      `gorm:"index;not null" json:"-"`
	CreatedAt              time.Time `json:"created_at"`
	UpdatedAt              time.Time `json:"updated_at"`
	Asset                  Asset     `json:"asset,omitempty"`
}

func (grant *StyleReferenceGrant) BeforeCreate(*gorm.DB) error {
	if grant.Version == 0 {
		grant.Version = 1
	}
	return ensurePublicID(&grant.PublicID)
}

// ModelFamilyReferenceAsset grants narrowly scoped geometry reuse. Appearance
// and identity are deliberately not valid roles.
type ModelFamilyReferenceAsset struct {
	ID                      uint        `gorm:"primaryKey" json:"-"`
	PublicID                string      `gorm:"size:36;uniqueIndex;not null" json:"public_id"`
	ModelFamilyID           uint        `gorm:"uniqueIndex:idx_family_reference_asset,priority:1;index;not null" json:"-"`
	AssetID                 uint        `gorm:"uniqueIndex:idx_family_reference_asset,priority:2;index;not null" json:"-"`
	Role                    string      `gorm:"uniqueIndex:idx_family_reference_asset,priority:3;size:32;index;not null" json:"role"`
	Version                 int         `gorm:"uniqueIndex:idx_family_reference_asset,priority:4;not null;default:1" json:"version"`
	AllowedAttributesJSON   []byte      `gorm:"type:json;not null" json:"allowed_attributes"`
	ForbiddenAttributesJSON []byte      `gorm:"type:json;not null" json:"forbidden_attributes"`
	DerivativeObjectKey     string      `gorm:"size:768;not null" json:"-"`
	DerivativeSHA256        string      `gorm:"size:64;index;not null" json:"derivative_sha256"`
	Status                  string      `gorm:"size:32;index;not null;default:approved" json:"status"`
	ReviewedByID            uint        `gorm:"index;not null" json:"-"`
	CreatedAt               time.Time   `json:"created_at"`
	UpdatedAt               time.Time   `json:"updated_at"`
	Asset                   Asset       `json:"asset,omitempty"`
	ModelFamily             ModelFamily `json:"model_family,omitempty"`
}

func (reference *ModelFamilyReferenceAsset) BeforeCreate(*gorm.DB) error {
	if err := ensurePublicID(&reference.PublicID); err != nil {
		return err
	}
	if len(reference.AllowedAttributesJSON) == 0 {
		reference.AllowedAttributesJSON = []byte(`[]`)
	}
	if len(reference.ForbiddenAttributesJSON) == 0 {
		reference.ForbiddenAttributesJSON = []byte(`[]`)
	}
	if reference.Version == 0 {
		reference.Version = 1
	}
	return nil
}
