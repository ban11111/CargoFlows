package models

import (
	"time"

	"gorm.io/gorm"
)

type AIReferencePurpose string

const (
	AIReferenceVisualStyle     AIReferencePurpose = "visual_style"
	AIReferenceUsageEffect     AIReferencePurpose = "usage_effect"
	AIReferenceCopyInspiration AIReferencePurpose = "copy_inspiration"
)

type AIReferenceSOP struct {
	ID          uint                    `gorm:"primaryKey" json:"-"`
	PublicID    string                  `gorm:"size:36;uniqueIndex;not null" json:"public_id"`
	CategoryID  uint                    `gorm:"index;not null" json:"category_id"`
	CreatedByID uint                    `gorm:"index;not null" json:"-"`
	Category    Category                `json:"category"`
	Versions    []AIReferenceSOPVersion `gorm:"foreignKey:AIReferenceSOPID" json:"versions"`
	CreatedAt   time.Time               `json:"created_at"`
	UpdatedAt   time.Time               `json:"updated_at"`
}

func (value *AIReferenceSOP) BeforeCreate(*gorm.DB) error { return ensurePublicID(&value.PublicID) }

type AIReferenceSOPVersion struct {
	ID                  uint              `gorm:"primaryKey" json:"-"`
	PublicID            string            `gorm:"size:36;uniqueIndex;not null" json:"public_id"`
	AIReferenceSOPID    uint              `gorm:"uniqueIndex:idx_ai_reference_sop_version;not null" json:"-"`
	VersionNumber       int               `gorm:"uniqueIndex:idx_ai_reference_sop_version;not null" json:"version_number"`
	NameZH              string            `gorm:"size:160;not null" json:"name_zh"`
	NameEN              string            `gorm:"size:160;not null" json:"name_en"`
	DescriptionZH       string            `gorm:"type:text;not null" json:"description_zh"`
	DescriptionEN       string            `gorm:"type:text;not null" json:"description_en"`
	Status              SOPVersionStatus  `gorm:"size:32;index;not null" json:"status"`
	CopiedFromVersionID *uint             `gorm:"index" json:"-"`
	PublishedByID       *uint             `gorm:"index" json:"-"`
	PublishedAt         *time.Time        `json:"published_at"`
	ArchivedAt          *time.Time        `json:"archived_at"`
	Items               []AIReferenceItem `gorm:"foreignKey:AIReferenceSOPVersionID" json:"items"`
	CreatedAt           time.Time         `json:"created_at"`
	UpdatedAt           time.Time         `json:"updated_at"`
}

func (value *AIReferenceSOPVersion) BeforeCreate(*gorm.DB) error {
	return ensurePublicID(&value.PublicID)
}

type AIReferenceItem struct {
	ID                      uint               `gorm:"primaryKey" json:"-"`
	PublicID                string             `gorm:"size:36;uniqueIndex;not null" json:"public_id"`
	AIReferenceSOPVersionID uint               `gorm:"uniqueIndex:idx_ai_reference_item_order;not null" json:"-"`
	SortOrder               int                `gorm:"uniqueIndex:idx_ai_reference_item_order;not null" json:"sort_order"`
	Purpose                 AIReferencePurpose `gorm:"size:32;index;not null" json:"purpose"`
	CaptionZH               string             `gorm:"size:500;not null" json:"caption_zh"`
	CaptionEN               string             `gorm:"size:500;not null" json:"caption_en"`
	AllowedGuidanceZH       string             `gorm:"type:text;not null" json:"allowed_guidance_zh"`
	AllowedGuidanceEN       string             `gorm:"type:text;not null" json:"allowed_guidance_en"`
	ForbiddenGuidanceZH     string             `gorm:"type:text;not null" json:"forbidden_guidance_zh"`
	ForbiddenGuidanceEN     string             `gorm:"type:text;not null" json:"forbidden_guidance_en"`
	SourceName              string             `gorm:"size:240;not null" json:"source_name"`
	SourceURL               string             `gorm:"size:1000;not null" json:"source_url"`
	ObjectKey               string             `gorm:"size:768;not null" json:"-"`
	ThumbnailObjectKey      string             `gorm:"size:768;not null" json:"-"`
	OriginalObjectKey       string             `gorm:"size:768;not null" json:"-"`
	MaskObjectKey           string             `gorm:"size:768;not null" json:"-"`
	MIMEType                string             `gorm:"size:80;not null" json:"mime_type"`
	Width                   int                `gorm:"not null" json:"width"`
	Height                  int                `gorm:"not null" json:"height"`
	ByteCount               int64              `gorm:"not null" json:"byte_count"`
	SHA256                  string             `gorm:"size:64;index;not null" json:"sha256"`
	RightsConfirmed         bool               `gorm:"not null" json:"rights_confirmed"`
	CreatedByID             uint               `gorm:"index;not null" json:"-"`
	CreatedAt               time.Time          `json:"created_at"`
	UpdatedAt               time.Time          `json:"updated_at"`
}

func (value *AIReferenceItem) BeforeCreate(*gorm.DB) error { return ensurePublicID(&value.PublicID) }

type AIReferenceUpload struct {
	ID                      uint               `gorm:"primaryKey" json:"-"`
	PublicID                string             `gorm:"size:36;uniqueIndex;not null" json:"-"`
	AIReferenceSOPVersionID uint               `gorm:"index;not null" json:"-"`
	CreatedByID             uint               `gorm:"index;not null" json:"-"`
	Purpose                 AIReferencePurpose `gorm:"size:32;not null" json:"-"`
	TemporaryKey            string             `gorm:"size:768;uniqueIndex;not null" json:"-"`
	ContentType             string             `gorm:"size:80;not null" json:"-"`
	MaskTemporaryKey        string             `gorm:"size:768;not null" json:"-"`
	MaskContentType         string             `gorm:"size:80;not null" json:"-"`
	ExpiresAt               time.Time          `gorm:"index;not null" json:"-"`
	ConsumedAt              *time.Time         `gorm:"index" json:"-"`
	CreatedAt               time.Time          `json:"-"`
}

func (value *AIReferenceUpload) BeforeCreate(*gorm.DB) error { return ensurePublicID(&value.PublicID) }
