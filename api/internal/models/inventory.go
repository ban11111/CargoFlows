package models

import (
	"time"

	"gorm.io/gorm"
)

type InventoryTransaction struct {
	ID             uint                       `gorm:"primaryKey" json:"-"`
	PublicID       string                     `gorm:"size:36;uniqueIndex;not null" json:"public_id"`
	Type           string                     `gorm:"size:32;index;not null" json:"type"`
	Status         string                     `gorm:"size:16;index;not null;default:draft" json:"status"`
	BusinessDate   time.Time                  `gorm:"type:date;index;not null" json:"business_date"`
	Note           string                     `gorm:"type:text" json:"note"`
	IdempotencyKey *string                    `gorm:"size:128;uniqueIndex:idx_inventory_actor_idempotency,priority:2" json:"-"`
	CreatedByID    uint                       `gorm:"index;uniqueIndex:idx_inventory_actor_idempotency,priority:1;not null" json:"-"`
	PostedByID     *uint                      `gorm:"index" json:"-"`
	ReversalOfID   *uint                      `gorm:"uniqueIndex" json:"-"`
	PostedAt       *time.Time                 `json:"posted_at"`
	CreatedAt      time.Time                  `json:"created_at"`
	UpdatedAt      time.Time                  `json:"updated_at"`
	Lines          []InventoryTransactionLine `json:"lines"`
	Charges        []InventoryCharge          `json:"charges"`
	CreatedBy      User                       `json:"created_by"`
	PostedBy       *User                      `json:"posted_by,omitempty"`
	ReversalOf     *InventoryTransaction      `json:"-"`
}

func (value *InventoryTransaction) BeforeCreate(*gorm.DB) error {
	return ensurePublicID(&value.PublicID)
}

type InventoryTransactionLine struct {
	ID                      uint       `gorm:"primaryKey" json:"-"`
	PublicID                string     `gorm:"size:36;uniqueIndex;not null" json:"public_id"`
	InventoryTransactionID  uint       `gorm:"index;not null" json:"-"`
	SKUID                   uint       `gorm:"index;not null" json:"-"`
	QuantityDelta           int        `gorm:"not null" json:"quantity_delta"`
	SourceCurrency          string     `gorm:"size:3;not null;default:SGD" json:"source_currency"`
	SourceUnitPrice         string     `gorm:"type:decimal(20,8);not null;default:0" json:"source_unit_price"`
	FXRateToSGD             string     `gorm:"type:decimal(20,8);not null;default:0" json:"fx_rate_to_sgd"`
	FXRateDate              *time.Time `gorm:"type:date" json:"fx_rate_date"`
	FXRateSource            string     `gorm:"size:32;not null;default:''" json:"fx_rate_source"`
	MerchandiseAmountSGD    string     `gorm:"type:decimal(20,8);not null;default:0" json:"merchandise_amount_sgd"`
	AllocatedChargesSGD     string     `gorm:"type:decimal(20,8);not null;default:0" json:"allocated_charges_sgd"`
	LandedUnitCostSGD       string     `gorm:"type:decimal(20,8);not null;default:0" json:"landed_unit_cost_sgd"`
	MovementCostSGD         string     `gorm:"type:decimal(20,8);not null;default:0" json:"movement_cost_sgd"`
	QuantityBefore          int        `gorm:"not null;default:0" json:"quantity_before"`
	QuantityAfter           int        `gorm:"not null;default:0" json:"quantity_after"`
	AverageCostBeforeSGD    string     `gorm:"type:decimal(20,8);not null;default:0" json:"average_cost_before_sgd"`
	AverageCostAfterSGD     string     `gorm:"type:decimal(20,8);not null;default:0" json:"average_cost_after_sgd"`
	InventoryValueBeforeSGD string     `gorm:"type:decimal(20,8);not null;default:0" json:"inventory_value_before_sgd"`
	InventoryValueAfterSGD  string     `gorm:"type:decimal(20,8);not null;default:0" json:"inventory_value_after_sgd"`
	CreatedAt               time.Time  `json:"created_at"`
	SKU                     SKU        `json:"sku"`
}

func (value *InventoryTransactionLine) BeforeCreate(*gorm.DB) error {
	return ensurePublicID(&value.PublicID)
}

type InventoryCharge struct {
	ID                     uint       `gorm:"primaryKey" json:"-"`
	PublicID               string     `gorm:"size:36;uniqueIndex;not null" json:"public_id"`
	InventoryTransactionID uint       `gorm:"index;not null" json:"-"`
	Type                   string     `gorm:"size:80;not null" json:"type"`
	SourceCurrency         string     `gorm:"size:3;not null;default:CNY" json:"source_currency"`
	SourceAmount           string     `gorm:"type:decimal(20,8);not null" json:"source_amount"`
	FXRateToSGD            string     `gorm:"type:decimal(20,8);not null;default:0" json:"fx_rate_to_sgd"`
	FXRateDate             *time.Time `gorm:"type:date" json:"fx_rate_date"`
	FXRateSource           string     `gorm:"size:32;not null;default:''" json:"fx_rate_source"`
	AmountSGD              string     `gorm:"type:decimal(20,8);not null;default:0" json:"amount_sgd"`
	CreatedAt              time.Time  `json:"created_at"`
}

func (value *InventoryCharge) BeforeCreate(*gorm.DB) error { return ensurePublicID(&value.PublicID) }

type ExchangeRate struct {
	ID             uint      `gorm:"primaryKey" json:"-"`
	RateDate       time.Time `gorm:"type:date;uniqueIndex:idx_exchange_rate,priority:1;not null" json:"rate_date"`
	SourceCurrency string    `gorm:"size:3;uniqueIndex:idx_exchange_rate,priority:2;not null" json:"source_currency"`
	TargetCurrency string    `gorm:"size:3;uniqueIndex:idx_exchange_rate,priority:3;not null" json:"target_currency"`
	Rate           string    `gorm:"type:decimal(20,8);not null" json:"rate"`
	Source         string    `gorm:"size:32;not null" json:"source"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}
