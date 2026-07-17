package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Role string

const (
	RoleAdmin        Role = "admin"
	RoleOperator     Role = "operator"
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
	ID           uint      `gorm:"primaryKey" json:"id"`
	Name         string    `gorm:"size:120;not null" json:"name"`
	Email        string    `gorm:"size:180;uniqueIndex;not null" json:"email"`
	PasswordHash string    `gorm:"size:255;not null" json:"-"`
	Role         Role      `gorm:"size:32;not null" json:"role"`
	Status       string    `gorm:"size:32;not null;default:active" json:"status"`
	LastSeenAt   time.Time `json:"last_seen_at"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type Product struct {
	ID              uint      `gorm:"primaryKey" json:"id"`
	CategoryID      uint      `gorm:"index" json:"category_id"`
	Name            string    `gorm:"size:180;not null" json:"name"`
	Brand           string    `gorm:"size:120" json:"brand"`
	Category        string    `gorm:"size:120;index" json:"category"`
	Description     string    `gorm:"type:text" json:"description"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	SKUs            []SKU     `json:"skus,omitempty"`
	CatalogCategory Category  `gorm:"foreignKey:CategoryID" json:"category_record"`
}

type SKU struct {
	ID                uint      `gorm:"primaryKey" json:"-"`
	PublicID          string    `gorm:"size:36;uniqueIndex;not null" json:"public_id"`
	ProductID         uint      `gorm:"index;not null" json:"product_id"`
	Code              string    `gorm:"size:80;uniqueIndex;not null" json:"code"`
	Color             string    `gorm:"size:80" json:"color"`
	Size              string    `gorm:"size:80" json:"size"`
	Barcode           string    `gorm:"size:120" json:"barcode"`
	Stock             int       `gorm:"not null;default:0" json:"stock"`
	LowStockThreshold int       `gorm:"not null;default:0" json:"low_stock_threshold"`
	PlatformTitle     string    `gorm:"size:240" json:"platform_title"`
	SellingPoints     string    `gorm:"type:text" json:"selling_points"`
	Status            string    `gorm:"size:32;not null;default:active" json:"status"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
	Product           Product   `json:"product"`
	Tags              []Tag     `gorm:"many2many:sku_tags;" json:"tags"`
}

type InventoryAdjustment struct {
	ID            uint      `gorm:"primaryKey" json:"id"`
	SKUID         uint      `gorm:"index;not null" json:"sku_id"`
	QuantityDelta int       `gorm:"not null" json:"quantity_delta"`
	Reason        string    `gorm:"size:180;not null" json:"reason"`
	Note          string    `gorm:"type:text" json:"note"`
	OperatorID    uint      `gorm:"index;not null" json:"operator_id"`
	CreatedAt     time.Time `json:"created_at"`
	Operator      User      `json:"operator"`
}

type PhotoSession struct {
	ID             uint       `gorm:"primaryKey" json:"-"`
	PublicID       string     `gorm:"size:36;uniqueIndex;not null" json:"public_id"`
	Code           string     `gorm:"size:80;uniqueIndex;not null" json:"code"`
	SKUID          uint       `gorm:"index;not null" json:"sku_id"`
	SOPVersionID   uint       `gorm:"index;not null" json:"-"`
	PhotographerID uint       `gorm:"index;not null" json:"photographer_id"`
	Status         string     `gorm:"size:32;not null;default:in_progress" json:"status"`
	SOPVersion     SOPVersion `json:"sop_version"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

type Asset struct {
	ID             uint         `gorm:"primaryKey" json:"-"`
	PublicID       string       `gorm:"size:36;uniqueIndex;not null" json:"public_id"`
	SKUID          uint         `gorm:"index;not null" json:"sku_id"`
	PhotoSessionID uint         `gorm:"index" json:"photo_session_id"`
	SOPViewID      uint         `gorm:"index" json:"sop_view_id"`
	ObjectKey      string       `gorm:"size:500;uniqueIndex;not null" json:"object_key"`
	OriginalURL    string       `gorm:"size:500;not null" json:"original_url"`
	ThumbnailURL   string       `gorm:"size:500" json:"thumbnail_url"`
	ReviewStatus   string       `gorm:"size:32;not null;default:pending" json:"review_status"`
	MIMEType       string       `gorm:"size:80;not null;default:'';<-:create" json:"mime_type"`
	Width          int          `gorm:"not null;default:0;<-:create" json:"width"`
	Height         int          `gorm:"not null;default:0;<-:create" json:"height"`
	ByteCount      int64        `gorm:"not null;default:0;<-:create" json:"byte_count"`
	SHA256         string       `gorm:"size:64;not null;default:'';<-:create" json:"sha256"`
	CapturedAt     time.Time    `json:"captured_at"`
	CreatedAt      time.Time    `json:"created_at"`
	UpdatedAt      time.Time    `json:"updated_at"`
	SKU            SKU          `json:"sku"`
	SOPView        SOPView      `json:"sop_view"`
	PhotoSession   PhotoSession `json:"photo_session"`
}

func (sku *SKU) BeforeCreate(*gorm.DB) error {
	if sku.PublicID == "" {
		sku.PublicID = uuid.NewString()
	}
	return nil
}

func (asset *Asset) BeforeCreate(*gorm.DB) error {
	if asset.PublicID == "" {
		asset.PublicID = uuid.NewString()
	}
	return nil
}

type AssetReview struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	AssetID    uint      `gorm:"index;not null" json:"asset_id"`
	ReviewerID uint      `gorm:"index;not null" json:"reviewer_id"`
	Status     string    `gorm:"size:32;not null" json:"status"`
	Reason     string    `gorm:"type:text" json:"reason"`
	CreatedAt  time.Time `json:"created_at"`
}
