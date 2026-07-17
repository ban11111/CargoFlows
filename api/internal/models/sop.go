package models

import "time"

type SOPVersionStatus string

const (
	SOPVersionDraft     SOPVersionStatus = "draft"
	SOPVersionPublished SOPVersionStatus = "published"
	SOPVersionArchived  SOPVersionStatus = "archived"
)

type SOPViewRole string

const (
	SOPViewReferenceFront SOPViewRole = "reference_front"
	SOPViewCapture        SOPViewRole = "capture"
)

type SOPViewKind string

const (
	SOPViewStandard SOPViewKind = "standard"
	SOPViewDetail   SOPViewKind = "detail"
)

type Composition struct {
	FrameOccupancy          float64 `json:"frame_occupancy"`
	AspectRatio             string  `json:"aspect_ratio"`
	AllowRotationCorrection bool    `json:"allow_rotation_correction"`
	AllowMirror             bool    `json:"allow_mirror"`
}

type CaptureSOP struct {
	ID          uint         `gorm:"primaryKey" json:"-"`
	PublicID    string       `gorm:"size:36;uniqueIndex;not null" json:"public_id"`
	CategoryID  uint         `gorm:"index;not null" json:"category_id"`
	CreatedByID uint         `gorm:"index;not null" json:"created_by_id"`
	Versions    []SOPVersion `json:"versions,omitempty"`
	Category    Category     `gorm:"foreignKey:CategoryID" json:"category"`
	CreatedAt   time.Time    `json:"created_at"`
	UpdatedAt   time.Time    `json:"updated_at"`
}

type SOPVersion struct {
	ID                  uint             `gorm:"primaryKey" json:"-"`
	PublicID            string           `gorm:"size:36;uniqueIndex;not null" json:"public_id"`
	CaptureSOPID        uint             `gorm:"uniqueIndex:idx_sop_version;not null" json:"-"`
	VersionNumber       int              `gorm:"uniqueIndex:idx_sop_version;not null" json:"version_number"`
	SchemaVersion       string           `gorm:"size:16;not null" json:"schema_version"`
	NameZH              string           `gorm:"size:160;not null" json:"-"`
	NameEN              string           `gorm:"size:160;not null" json:"-"`
	DescriptionZH       string           `gorm:"type:text;not null" json:"-"`
	DescriptionEN       string           `gorm:"type:text;not null" json:"-"`
	Status              SOPVersionStatus `gorm:"size:32;index;not null" json:"status"`
	CoordinateSystem    string           `gorm:"size:32;not null" json:"-"`
	CopiedFromVersionID *uint            `gorm:"index" json:"-"`
	PublishedAt         *time.Time       `json:"published_at"`
	Views               []SOPView        `gorm:"foreignKey:SOPVersionID" json:"views"`
	CreatedAt           time.Time        `json:"created_at"`
	UpdatedAt           time.Time        `json:"updated_at"`
}

type SOPView struct {
	ID              uint                    `gorm:"primaryKey" json:"-"`
	PublicID        string                  `gorm:"size:36;uniqueIndex;not null" json:"public_id"`
	SOPVersionID    uint                    `gorm:"uniqueIndex:idx_version_sequence;not null" json:"-"`
	Sequence        int                     `gorm:"uniqueIndex:idx_version_sequence;not null" json:"sequence"`
	Role            SOPViewRole             `gorm:"size:32;not null" json:"role"`
	ViewKind        SOPViewKind             `gorm:"size:32;not null" json:"view_kind"`
	PresetKey       string                  `gorm:"size:64" json:"preset_key"`
	NameZH          string                  `gorm:"size:120;not null" json:"-"`
	NameEN          string                  `gorm:"size:120;not null" json:"-"`
	InstructionZH   string                  `gorm:"type:text;not null" json:"-"`
	InstructionEN   string                  `gorm:"type:text;not null" json:"-"`
	Required        bool                    `gorm:"not null" json:"required"`
	CameraPositionX float64                 `gorm:"type:decimal(10,6);not null" json:"-"`
	CameraPositionY float64                 `gorm:"type:decimal(10,6);not null" json:"-"`
	CameraPositionZ float64                 `gorm:"type:decimal(10,6);not null" json:"-"`
	ImageUpX        float64                 `gorm:"type:decimal(10,6);not null" json:"-"`
	ImageUpY        float64                 `gorm:"type:decimal(10,6);not null" json:"-"`
	ImageUpZ        float64                 `gorm:"type:decimal(10,6);not null" json:"-"`
	TargetX         float64                 `gorm:"type:decimal(10,6);not null" json:"-"`
	TargetY         float64                 `gorm:"type:decimal(10,6);not null" json:"-"`
	TargetZ         float64                 `gorm:"type:decimal(10,6);not null" json:"-"`
	Composition     Composition             `gorm:"serializer:json;type:json;not null" json:"composition"`
	ReferenceImages []SOPViewReferenceImage `json:"reference_images"`
	CreatedAt       time.Time               `json:"created_at"`
	UpdatedAt       time.Time               `json:"updated_at"`
}

type SOPViewReferenceImage struct {
	ID           uint      `gorm:"primaryKey" json:"-"`
	PublicID     string    `gorm:"size:36;uniqueIndex;not null" json:"public_id"`
	SOPViewID    uint      `gorm:"uniqueIndex:idx_view_reference_order;not null" json:"-"`
	ObjectKey    string    `gorm:"size:500;not null" json:"-"`
	ThumbnailURL string    `gorm:"size:500;not null" json:"-"`
	SortOrder    int       `gorm:"uniqueIndex:idx_view_reference_order;not null" json:"sort_order"`
	CaptionZH    string    `gorm:"size:240;not null" json:"-"`
	CaptionEN    string    `gorm:"size:240;not null" json:"-"`
	CreatedAt    time.Time `json:"created_at"`
}

type SOPReferenceUpload struct {
	ID           uint       `gorm:"primaryKey" json:"-"`
	PublicID     string     `gorm:"size:36;uniqueIndex;not null" json:"-"`
	SOPVersionID uint       `gorm:"index;not null" json:"-"`
	SOPViewID    uint       `gorm:"index;not null" json:"-"`
	CreatedByID  uint       `gorm:"index;not null" json:"-"`
	TemporaryKey string     `gorm:"size:500;uniqueIndex;not null" json:"-"`
	ContentType  string     `gorm:"size:80;not null" json:"-"`
	ExpiresAt    time.Time  `gorm:"index;not null" json:"-"`
	ConsumedAt   *time.Time `gorm:"index" json:"-"`
	CreatedAt    time.Time  `json:"-"`
}
