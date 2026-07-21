package models

import (
	"time"

	"gorm.io/gorm"
)

type Role string

const (
	RoleSuperAdmin Role = "super_admin"
	RoleAdmin      Role = "admin"
	RoleOperator   Role = "operator"
	// Legacy roles are retained only so old rows can be migrated safely.
	RolePhotographer Role = "photographer"
	RoleViewer       Role = "viewer"
)

type Category struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Name      string    `gorm:"size:120;uniqueIndex;not null" json:"name"`
	NameEN    string    `gorm:"size:120;not null;default:''" json:"name_en"`
	IsSystem  bool      `gorm:"not null;default:false" json:"is_system"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Tag struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Name      string    `gorm:"size:80;uniqueIndex;not null" json:"name"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type User struct {
	ID                 uint           `gorm:"primaryKey" json:"-"`
	PublicID           string         `gorm:"size:36;uniqueIndex" json:"public_id"`
	Name               string         `gorm:"size:120;not null" json:"name"`
	Email              string         `gorm:"size:180;uniqueIndex;not null" json:"email"`
	PasswordHash       string         `gorm:"size:255;not null" json:"-"`
	Role               Role           `gorm:"size:32;not null" json:"role"`
	Status             string         `gorm:"size:32;not null;default:active" json:"status"`
	MustChangePassword bool           `gorm:"not null;default:false" json:"must_change_password"`
	SessionVersion     uint           `gorm:"not null;default:1" json:"-"`
	LastSeenAt         *time.Time     `json:"last_seen_at"`
	CreatedAt          time.Time      `json:"created_at"`
	UpdatedAt          time.Time      `json:"updated_at"`
	DeletedAt          gorm.DeletedAt `gorm:"index" json:"-"`
}

func (user *User) BeforeCreate(*gorm.DB) error {
	if err := ensurePublicID(&user.PublicID); err != nil {
		return err
	}
	if user.SessionVersion == 0 {
		user.SessionVersion = 1
	}
	return nil
}

type Product struct {
	ID              uint      `gorm:"primaryKey" json:"id"`
	BrandID         *uint     `gorm:"index" json:"-"`
	CategoryID      uint      `gorm:"index" json:"category_id"`
	Name            string    `gorm:"size:180;not null" json:"name"`
	Brand           string    `gorm:"size:120" json:"brand"`
	Category        string    `gorm:"size:120;index" json:"category"`
	Description     string    `gorm:"type:text" json:"description"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	SKUs            []SKU     `json:"skus,omitempty"`
	CatalogCategory Category  `gorm:"foreignKey:CategoryID" json:"category_record"`
	BrandRecord     *Brand    `gorm:"foreignKey:BrandID" json:"-"`
}

type Brand struct {
	ID        uint        `gorm:"primaryKey" json:"-"`
	PublicID  string      `gorm:"size:36;uniqueIndex;not null" json:"public_id"`
	Name      string      `gorm:"size:120;not null" json:"name"`
	NameKey   string      `gorm:"size:120;uniqueIndex;not null" json:"-"`
	CreatedAt time.Time   `json:"created_at"`
	UpdatedAt time.Time   `json:"updated_at"`
	Icons     []BrandIcon `json:"icons,omitempty"`
}

func (brand *Brand) BeforeCreate(*gorm.DB) error { return ensurePublicID(&brand.PublicID) }

type BrandIcon struct {
	ID        uint      `gorm:"primaryKey" json:"-"`
	PublicID  string    `gorm:"size:36;uniqueIndex;not null" json:"public_id"`
	BrandID   uint      `gorm:"index;not null" json:"-"`
	Name      string    `gorm:"size:120;not null" json:"name"`
	Notes     string    `gorm:"size:500;not null;default:''" json:"notes"`
	ObjectKey string    `gorm:"size:500;uniqueIndex;not null" json:"-"`
	MIMEType  string    `gorm:"size:80;not null" json:"mime_type"`
	Width     int       `gorm:"not null" json:"width"`
	Height    int       `gorm:"not null" json:"height"`
	ByteCount int64     `gorm:"not null" json:"byte_count"`
	SHA256    string    `gorm:"size:64;not null" json:"sha256"`
	SortOrder int       `gorm:"not null;default:1" json:"sort_order"`
	Status    string    `gorm:"size:32;index;not null;default:active" json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Brand     Brand     `json:"-"`
}

func (icon *BrandIcon) BeforeCreate(*gorm.DB) error { return ensurePublicID(&icon.PublicID) }

type BrandIconUpload struct {
	ID           uint       `gorm:"primaryKey" json:"-"`
	PublicID     string     `gorm:"size:36;uniqueIndex;not null" json:"-"`
	BrandID      uint       `gorm:"index;not null" json:"-"`
	CreatedByID  uint       `gorm:"index;not null" json:"-"`
	TemporaryKey string     `gorm:"size:500;uniqueIndex;not null" json:"-"`
	ContentType  string     `gorm:"size:80;not null" json:"-"`
	ExpiresAt    time.Time  `gorm:"index;not null" json:"-"`
	ConsumedAt   *time.Time `gorm:"index" json:"-"`
	CreatedAt    time.Time  `json:"-"`
}

func (upload *BrandIconUpload) BeforeCreate(*gorm.DB) error { return ensurePublicID(&upload.PublicID) }

type SKU struct {
	ID                    uint      `gorm:"primaryKey" json:"-"`
	PublicID              string    `gorm:"size:36;uniqueIndex;not null" json:"public_id"`
	ProductID             uint      `gorm:"index;not null" json:"-"`
	Code                  string    `gorm:"size:80;uniqueIndex;not null" json:"code"`
	Color                 string    `gorm:"size:80" json:"color"`
	Size                  string    `gorm:"size:80" json:"size"`
	CompatibleDeviceModel string    `gorm:"size:120" json:"compatible_device_model"`
	Barcode               string    `gorm:"size:120" json:"barcode"`
	Stock                 int       `gorm:"not null;default:0" json:"stock"`
	AverageUnitCostSGD    string    `gorm:"type:decimal(20,8);not null;default:0" json:"average_unit_cost_sgd"`
	InventoryValueSGD     string    `gorm:"type:decimal(20,8);not null;default:0" json:"inventory_value_sgd"`
	ZeroCostOpening       bool      `gorm:"not null;default:false" json:"zero_cost_opening"`
	LowStockThreshold     int       `gorm:"not null;default:0" json:"low_stock_threshold"`
	PlatformTitle         string    `gorm:"size:240" json:"platform_title"`
	SellingPoints         string    `gorm:"type:text" json:"selling_points"`
	Status                string    `gorm:"size:32;not null;default:active" json:"status"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
	Product               Product   `json:"product"`
	Tags                  []Tag     `gorm:"many2many:sku_tags;" json:"tags"`
}

type InventoryAdjustment struct {
	ID            uint      `gorm:"primaryKey" json:"-"`
	SKUID         uint      `gorm:"index;not null" json:"-"`
	QuantityDelta int       `gorm:"not null" json:"quantity_delta"`
	Reason        string    `gorm:"size:180;not null" json:"reason"`
	Note          string    `gorm:"type:text" json:"note"`
	OperatorID    uint      `gorm:"index;not null" json:"-"`
	CreatedAt     time.Time `json:"created_at"`
	Operator      User      `json:"operator"`
}

type PhotoSession struct {
	ID             uint       `gorm:"primaryKey" json:"-"`
	PublicID       string     `gorm:"size:36;uniqueIndex;not null" json:"public_id"`
	Code           string     `gorm:"size:80;uniqueIndex;not null" json:"code"`
	SKUID          uint       `gorm:"index;not null" json:"-"`
	SOPVersionID   uint       `gorm:"index;not null" json:"-"`
	PhotographerID uint       `gorm:"index;not null" json:"-"`
	Status         string     `gorm:"size:32;not null;default:in_progress" json:"status"`
	SOPVersion     SOPVersion `json:"sop_version"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

type Asset struct {
	ID                    uint         `gorm:"primaryKey" json:"-"`
	PublicID              string       `gorm:"size:36;uniqueIndex;not null" json:"public_id"`
	SKUID                 uint         `gorm:"index;index:idx_assets_review_queue,priority:1;not null" json:"-"`
	PhotoSessionID        uint         `gorm:"index" json:"-"`
	SOPViewID             uint         `gorm:"index" json:"-"`
	UploadID              *string      `gorm:"size:36;uniqueIndex" json:"-"`
	ObjectKey             string       `gorm:"size:500;uniqueIndex;not null" json:"-"`
	OriginalURL           string       `gorm:"size:500;not null" json:"-"`
	ThumbnailURL          string       `gorm:"size:500" json:"-"`
	ReviewStatus          string       `gorm:"size:32;index:idx_assets_review_queue,priority:2;not null;default:pending" json:"review_status"`
	MIMEType              string       `gorm:"size:80;not null;default:'';<-:create" json:"mime_type"`
	Width                 int          `gorm:"not null;default:0;<-:create" json:"width"`
	Height                int          `gorm:"not null;default:0;<-:create" json:"height"`
	ByteCount             int64        `gorm:"not null;default:0;<-:create" json:"byte_count"`
	SHA256                string       `gorm:"size:64;not null;default:'';<-:create" json:"sha256"`
	OriginType            string       `gorm:"size:32;index;not null;default:uploaded" json:"origin_type"`
	SourceAIImageResultID *uint        `gorm:"uniqueIndex" json:"-"`
	ProvenanceJSON        []byte       `gorm:"type:json;not null" json:"-"`
	CapturedAt            time.Time    `gorm:"index:idx_assets_review_queue,priority:3" json:"captured_at"`
	CreatedAt             time.Time    `json:"created_at"`
	UpdatedAt             time.Time    `json:"updated_at"`
	SKU                   SKU          `json:"sku"`
	SOPView               SOPView      `json:"sop_view"`
	PhotoSession          PhotoSession `json:"photo_session"`
}

func (sku *SKU) BeforeCreate(*gorm.DB) error {
	return ensurePublicID(&sku.PublicID)
}

func (asset *Asset) BeforeCreate(*gorm.DB) error {
	if err := ensurePublicID(&asset.PublicID); err != nil {
		return err
	}
	if asset.OriginType == "" {
		asset.OriginType = "uploaded"
	}
	if len(asset.ProvenanceJSON) == 0 {
		asset.ProvenanceJSON = []byte(`{}`)
	}
	return nil
}

type AssetReview struct {
	ID         uint      `gorm:"primaryKey" json:"-"`
	AssetID    uint      `gorm:"index;not null" json:"-"`
	ReviewerID uint      `gorm:"index;not null" json:"-"`
	Status     string    `gorm:"size:32;not null" json:"status"`
	Reason     string    `gorm:"type:text" json:"reason"`
	CreatedAt  time.Time `json:"created_at"`
}
