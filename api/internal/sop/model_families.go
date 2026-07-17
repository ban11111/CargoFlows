package sop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"cargoflow/api/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrModelFamilyNotFound       = errors.New("model family not found")
	ErrModelFamilyArchived       = errors.New("model family is archived")
	ErrModelFamilyInvalid        = errors.New("model family input is invalid")
	ErrSKUNotFound               = errors.New("SKU not found")
	ErrModelFamilyMemberNotFound = errors.New("model family member not found")
	ErrSKUAlreadyInModelFamily   = errors.New("SKU already belongs to an active model family")
	ErrMembershipConflict        = errors.New("model family membership conflict")
	ErrModelCodeTaken            = errors.New("model code is already in use")
)

var allowedVariationDimensions = map[string]struct{}{
	"color": {}, "material": {}, "finish": {}, "texture": {}, "trim": {},
	"ports": {}, "controls": {}, "labels": {}, "accessories": {}, "packaging": {}, "other": {},
}

// ModelFamilyService owns only lifecycle state. It deliberately receives public
// identifiers so application callers never need catalogue primary keys.
type ModelFamilyService struct {
	db *gorm.DB
	// The database constraint is the cross-process guard. This narrow mutex also
	// makes SQLite-based local/test execution deterministic where FOR UPDATE is a
	// no-op and concurrent writers otherwise return a transient lock error.
	membershipMu sync.Mutex
}

func NewModelFamilyService(db *gorm.DB) *ModelFamilyService { return &ModelFamilyService{db: db} }

type CreateModelFamilyInput struct {
	Brand, NameZH, NameEN, ModelCode string
	CommonStructure                  json.RawMessage
	VariationDimensions              []string
	CreatedByID                      uint
}

type UpdateModelFamilyInput struct {
	Brand, NameZH, NameEN, ModelCode *string
	CommonStructure                  json.RawMessage
	VariationDimensions              *[]string
	Status                           *models.ModelFamilyStatus
	UpdatedByID                      uint
}

func (s *ModelFamilyService) Create(ctx context.Context, input CreateModelFamilyInput) (*models.ModelFamily, error) {
	common, dimensions, err := validateModelFamilyInput(input.Brand, input.NameZH, input.NameEN, input.ModelCode, input.CommonStructure, input.VariationDimensions)
	if err != nil {
		return nil, err
	}
	created := models.ModelFamily{PublicID: uuid.NewString(), Brand: strings.TrimSpace(input.Brand), NameZH: strings.TrimSpace(input.NameZH), NameEN: strings.TrimSpace(input.NameEN), ModelCode: strings.TrimSpace(input.ModelCode), CommonStructureJSON: common, VariationDimensionsJSON: dimensions, Status: models.ModelFamilyActive, CreatedByID: input.CreatedByID}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&created).Error; err != nil {
			if isUniqueViolation(err) {
				return ErrModelCodeTaken
			}
			return err
		}
		return writeModelFamilyAudit(tx, input.CreatedByID, "model_family.created", created.PublicID, map[string]string{"action": "create", "model_family_id": created.PublicID})
	})
	if err != nil {
		return nil, err
	}
	return &created, nil
}

func (s *ModelFamilyService) Update(ctx context.Context, publicID string, input UpdateModelFamilyInput) (*models.ModelFamily, error) {
	var result models.ModelFamily
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("public_id = ?", publicID).First(&result).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrModelFamilyNotFound
			}
			return err
		}
		if result.Status == models.ModelFamilyArchived {
			return ErrModelFamilyArchived
		}
		updates := map[string]any{}
		if input.Brand != nil {
			value, err := requiredModelFamilyText(*input.Brand, 120)
			if err != nil {
				return err
			}
			updates["brand"] = value
		}
		if input.NameZH != nil {
			value, err := requiredModelFamilyText(*input.NameZH, 180)
			if err != nil {
				return err
			}
			updates["name_zh"] = value
		}
		if input.NameEN != nil {
			value, err := requiredModelFamilyText(*input.NameEN, 180)
			if err != nil {
				return err
			}
			updates["name_en"] = value
		}
		if input.ModelCode != nil {
			value, err := requiredModelFamilyText(*input.ModelCode, 120)
			if err != nil {
				return err
			}
			updates["model_code"] = value
		}
		if input.CommonStructure != nil {
			common, err := validateCommonStructure(input.CommonStructure)
			if err != nil {
				return err
			}
			updates["common_structure_json"] = common
		}
		if input.VariationDimensions != nil {
			dimensions, err := normalizeVariationDimensions(*input.VariationDimensions)
			if err != nil {
				return err
			}
			updates["variation_dimensions_json"] = dimensions
		}
		if input.Status != nil {
			if *input.Status != models.ModelFamilyActive && *input.Status != models.ModelFamilyArchived {
				return ErrModelFamilyInvalid
			}
			updates["status"] = *input.Status
		}
		if len(updates) == 0 {
			return ErrModelFamilyInvalid
		}
		if err := tx.Model(&result).Updates(updates).Error; err != nil {
			if isUniqueViolation(err) {
				return ErrModelCodeTaken
			}
			return err
		}
		if err := tx.First(&result, result.ID).Error; err != nil {
			return err
		}
		action := "update"
		if input.Status != nil && *input.Status == models.ModelFamilyArchived {
			action = "archive"
		}
		return writeModelFamilyAudit(tx, input.UpdatedByID, "model_family."+action, result.PublicID, map[string]string{"action": action, "model_family_id": result.PublicID})
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (s *ModelFamilyService) AddMember(ctx context.Context, familyPublicID, skuPublicID string, actorID uint) (*models.ModelFamilyMember, error) {
	s.membershipMu.Lock()
	defer s.membershipMu.Unlock()
	var member models.ModelFamilyMember
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var family models.ModelFamily
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("public_id = ?", familyPublicID).First(&family).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrModelFamilyNotFound
			}
			return err
		}
		if family.Status == models.ModelFamilyArchived {
			return ErrModelFamilyArchived
		}
		var sku models.SKU
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("public_id = ?", skuPublicID).First(&sku).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrSKUNotFound
			}
			return err
		}
		var existing models.ModelFamilyMember
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("sk_uid = ? AND removed_at IS NULL", sku.ID).First(&existing).Error
		if err == nil {
			return ErrSKUAlreadyInModelFamily
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		member = models.ModelFamilyMember{PublicID: uuid.NewString(), ModelFamilyID: family.ID, SKUID: sku.ID, AddedByID: actorID}
		if err := tx.Create(&member).Error; err != nil {
			if isUniqueViolation(err) {
				return ErrSKUAlreadyInModelFamily
			}
			return err
		}
		return writeModelFamilyAudit(tx, actorID, "model_family.member_added", family.PublicID, map[string]string{"action": "add_member", "model_family_id": family.PublicID, "member_id": member.PublicID, "sku_id": sku.PublicID})
	})
	if err != nil {
		return nil, err
	}
	return &member, nil
}

func (s *ModelFamilyService) RemoveMember(ctx context.Context, familyPublicID, memberPublicID string, actorID uint) error {
	s.membershipMu.Lock()
	defer s.membershipMu.Unlock()
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var family models.ModelFamily
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("public_id = ?", familyPublicID).First(&family).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrModelFamilyNotFound
			}
			return err
		}
		if family.Status == models.ModelFamilyArchived {
			return ErrModelFamilyArchived
		}
		var member models.ModelFamilyMember
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("public_id = ? AND model_family_id = ? AND removed_at IS NULL", memberPublicID, family.ID).First(&member).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrModelFamilyMemberNotFound
			}
			return err
		}
		if err := tx.Model(&member).Updates(map[string]any{"removed_at": gorm.Expr("CURRENT_TIMESTAMP"), "removed_by_id": actorID, "active_guard": nil}).Error; err != nil {
			return err
		}
		return writeModelFamilyAudit(tx, actorID, "model_family.member_removed", family.PublicID, map[string]string{"action": "remove_member", "model_family_id": family.PublicID, "member_id": member.PublicID})
	})
}

func (s *ModelFamilyService) Get(ctx context.Context, publicID string) (*models.ModelFamily, error) {
	var result models.ModelFamily
	err := s.db.WithContext(ctx).Where("public_id = ?", publicID).Preload("Members", func(db *gorm.DB) *gorm.DB { return db.Order("created_at ASC") }).First(&result).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrModelFamilyNotFound
	}
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (s *ModelFamilyService) List(ctx context.Context) ([]models.ModelFamily, error) {
	var families []models.ModelFamily
	if err := s.db.WithContext(ctx).Order("created_at ASC").Find(&families).Error; err != nil {
		return nil, err
	}
	return families, nil
}

func validateModelFamilyInput(brand, nameZH, nameEN, modelCode string, common json.RawMessage, dimensions []string) (json.RawMessage, json.RawMessage, error) {
	if _, err := requiredModelFamilyText(brand, 120); err != nil {
		return nil, nil, err
	}
	if _, err := requiredModelFamilyText(nameZH, 180); err != nil {
		return nil, nil, err
	}
	if _, err := requiredModelFamilyText(nameEN, 180); err != nil {
		return nil, nil, err
	}
	if _, err := requiredModelFamilyText(modelCode, 120); err != nil {
		return nil, nil, ErrModelFamilyInvalid
	}
	value, err := validateCommonStructure(common)
	if err != nil {
		return nil, nil, err
	}
	dimensionJSON, err := normalizeVariationDimensions(dimensions)
	if err != nil {
		return nil, nil, err
	}
	return value, dimensionJSON, nil
}

func requiredModelFamilyText(value string, maxBytes int) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > maxBytes {
		return "", ErrModelFamilyInvalid
	}
	return value, nil
}

func validateCommonStructure(raw json.RawMessage) (json.RawMessage, error) {
	var value struct {
		Schema     string   `json:"schema"`
		Invariants []string `json:"invariants"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil || value.Schema != "model_family_common_structure_v1" || len(value.Invariants) == 0 {
		return nil, ErrModelFamilyInvalid
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, ErrModelFamilyInvalid
	}
	for _, invariant := range value.Invariants {
		if strings.TrimSpace(invariant) == "" {
			return nil, ErrModelFamilyInvalid
		}
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("marshal common structure: %w", err)
	}
	return canonical, nil
}

func normalizeVariationDimensions(dimensions []string) (json.RawMessage, error) {
	if len(dimensions) == 0 {
		return nil, ErrModelFamilyInvalid
	}
	seen := make(map[string]struct{}, len(dimensions))
	normalized := make([]string, 0, len(dimensions))
	for _, value := range dimensions {
		value = strings.TrimSpace(strings.ToLower(value))
		if _, allowed := allowedVariationDimensions[value]; !allowed {
			return nil, ErrModelFamilyInvalid
		}
		if _, duplicate := seen[value]; duplicate {
			return nil, ErrModelFamilyInvalid
		}
		seen[value] = struct{}{}
		normalized = append(normalized, value)
	}
	encoded, err := json.Marshal(normalized)
	return encoded, err
}

func writeModelFamilyAudit(tx *gorm.DB, actorID uint, eventType, entityPublicID string, metadata map[string]string) error {
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	actor := actorID
	return tx.Create(&models.AIAuditEvent{PublicID: uuid.NewString(), EventType: eventType, EntityType: "model_family", EntityPublicID: entityPublicID, ActorID: &actor, MetadataJSON: encoded}).Error
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	value := strings.ToLower(err.Error())
	return strings.Contains(value, "unique constraint") || strings.Contains(value, "duplicate entry") || strings.Contains(value, "duplicate key")
}
