package app

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"cargoflow/api/internal/models"
	"cargoflow/api/internal/sop"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type createModelFamilyRequest struct {
	Brand               string          `json:"brand"`
	NameZH              string          `json:"name_zh"`
	NameEN              string          `json:"name_en"`
	ModelCode           string          `json:"model_code"`
	CommonStructure     json.RawMessage `json:"common_structure"`
	VariationDimensions []string        `json:"variation_dimensions"`
}

type updateModelFamilyRequest struct {
	Brand               *string                   `json:"brand"`
	NameZH              *string                   `json:"name_zh"`
	NameEN              *string                   `json:"name_en"`
	ModelCode           *string                   `json:"model_code"`
	CommonStructure     json.RawMessage           `json:"common_structure"`
	VariationDimensions *[]string                 `json:"variation_dimensions"`
	Status              *models.ModelFamilyStatus `json:"status"`
}

type addModelFamilyMemberRequest struct {
	SKUID string `json:"sku_id"`
}

type modelFamilyMemberDTO struct {
	PublicID  string     `json:"public_id"`
	SKUID     string     `json:"sku_id"`
	RemovedAt *time.Time `json:"removed_at"`
	CreatedAt time.Time  `json:"created_at"`
}

type modelFamilyDTO struct {
	PublicID            string                   `json:"public_id"`
	Brand               string                   `json:"brand"`
	NameZH              string                   `json:"name_zh"`
	NameEN              string                   `json:"name_en"`
	ModelCode           string                   `json:"model_code"`
	CommonStructure     json.RawMessage          `json:"common_structure"`
	VariationDimensions json.RawMessage          `json:"variation_dimensions"`
	Status              models.ModelFamilyStatus `json:"status"`
	CreatedAt           time.Time                `json:"created_at"`
	UpdatedAt           time.Time                `json:"updated_at"`
	Members             []modelFamilyMemberDTO   `json:"members,omitempty"`
}

func (s *Server) createModelFamily(c *gin.Context) {
	var req createModelFamilyRequest
	if err := decodeJSONStrict(c, &req); err != nil {
		respondModelFamilyBadRequest(c, err)
		return
	}
	family, err := sop.NewModelFamilyService(s.db).Create(c.Request.Context(), sop.CreateModelFamilyInput{Brand: req.Brand, NameZH: req.NameZH, NameEN: req.NameEN, ModelCode: req.ModelCode, CommonStructure: req.CommonStructure, VariationDimensions: req.VariationDimensions, CreatedByID: currentUser(c).ID})
	if err != nil {
		respondModelFamilyError(c, err)
		return
	}
	c.JSON(http.StatusCreated, modelFamilyDTOFromModel(*family, nil))
}

func (s *Server) updateModelFamily(c *gin.Context) {
	if !requireUUIDParam(c, "family_id") {
		return
	}
	var req updateModelFamilyRequest
	if err := decodeJSONStrict(c, &req); err != nil {
		respondModelFamilyBadRequest(c, err)
		return
	}
	family, err := sop.NewModelFamilyService(s.db).Update(c.Request.Context(), c.Param("family_id"), sop.UpdateModelFamilyInput{Brand: req.Brand, NameZH: req.NameZH, NameEN: req.NameEN, ModelCode: req.ModelCode, CommonStructure: req.CommonStructure, VariationDimensions: req.VariationDimensions, Status: req.Status, UpdatedByID: currentUser(c).ID})
	if err != nil {
		respondModelFamilyError(c, err)
		return
	}
	c.JSON(http.StatusOK, modelFamilyDTOFromModel(*family, nil))
}

func (s *Server) addModelFamilyMember(c *gin.Context) {
	if !requireUUIDParam(c, "family_id") {
		return
	}
	var req addModelFamilyMemberRequest
	if err := decodeJSONStrict(c, &req); err != nil || !isUUID(req.SKUID) {
		respondModelFamilyBadRequest(c, errOr(err, "sku_id must be a UUID"))
		return
	}
	member, err := sop.NewModelFamilyService(s.db).AddMember(c.Request.Context(), c.Param("family_id"), req.SKUID, currentUser(c).ID)
	if err != nil {
		respondModelFamilyError(c, err)
		return
	}
	c.JSON(http.StatusCreated, modelFamilyMemberDTO{PublicID: member.PublicID, SKUID: req.SKUID, RemovedAt: member.RemovedAt, CreatedAt: member.CreatedAt})
}

func (s *Server) removeModelFamilyMember(c *gin.Context) {
	if !requireUUIDParam(c, "family_id") || !requireUUIDParam(c, "member_id") {
		return
	}
	if err := sop.NewModelFamilyService(s.db).RemoveMember(c.Request.Context(), c.Param("family_id"), c.Param("member_id"), currentUser(c).ID); err != nil {
		respondModelFamilyError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (s *Server) getModelFamily(c *gin.Context) {
	if !requireUUIDParam(c, "family_id") {
		return
	}
	family, err := sop.NewModelFamilyService(s.db).Get(c.Request.Context(), c.Param("family_id"))
	if err != nil {
		respondModelFamilyError(c, err)
		return
	}
	dto, err := s.modelFamilyDTO(*family)
	if err != nil {
		respondModelFamilyError(c, err)
		return
	}
	c.JSON(http.StatusOK, dto)
}

func (s *Server) listModelFamilies(c *gin.Context) {
	families, err := sop.NewModelFamilyService(s.db).List(c.Request.Context())
	if err != nil {
		respondModelFamilyError(c, err)
		return
	}
	data := make([]modelFamilyDTO, 0, len(families))
	for _, family := range families {
		data = append(data, modelFamilyDTOFromModel(family, nil))
	}
	c.JSON(http.StatusOK, gin.H{"data": data})
}

func (s *Server) modelFamilyDTO(family models.ModelFamily) (modelFamilyDTO, error) {
	memberSKUs := make(map[uint]string, len(family.Members))
	if len(family.Members) > 0 {
		ids := make([]uint, 0, len(family.Members))
		for _, member := range family.Members {
			ids = append(ids, member.SKUID)
		}
		var skus []models.SKU
		if err := s.db.Select("id", "public_id").Where("id IN ?", ids).Find(&skus).Error; err != nil {
			return modelFamilyDTO{}, err
		}
		for _, sku := range skus {
			memberSKUs[sku.ID] = sku.PublicID
		}
		if len(memberSKUs) != len(family.Members) {
			return modelFamilyDTO{}, gorm.ErrRecordNotFound
		}
	}
	return modelFamilyDTOFromModel(family, memberSKUs), nil
}

func modelFamilyDTOFromModel(family models.ModelFamily, memberSKUs map[uint]string) modelFamilyDTO {
	dto := modelFamilyDTO{PublicID: family.PublicID, Brand: family.Brand, NameZH: family.NameZH, NameEN: family.NameEN, ModelCode: family.ModelCode, CommonStructure: append(json.RawMessage(nil), family.CommonStructureJSON...), VariationDimensions: append(json.RawMessage(nil), family.VariationDimensionsJSON...), Status: family.Status, CreatedAt: family.CreatedAt, UpdatedAt: family.UpdatedAt}
	if memberSKUs != nil {
		dto.Members = make([]modelFamilyMemberDTO, 0, len(family.Members))
		for _, member := range family.Members {
			dto.Members = append(dto.Members, modelFamilyMemberDTO{PublicID: member.PublicID, SKUID: memberSKUs[member.SKUID], RemovedAt: member.RemovedAt, CreatedAt: member.CreatedAt})
		}
	}
	return dto
}

func respondModelFamilyBadRequest(c *gin.Context, err error) {
	c.JSON(http.StatusBadRequest, gin.H{"code": "invalid_request", "message": err.Error()})
}
func respondModelFamilyError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, sop.ErrModelFamilyNotFound), errors.Is(err, sop.ErrSKUNotFound), errors.Is(err, sop.ErrModelFamilyMemberNotFound), errors.Is(err, gorm.ErrRecordNotFound):
		c.JSON(http.StatusNotFound, gin.H{"code": "not_found", "message": "resource not found"})
	case errors.Is(err, sop.ErrSKUAlreadyInModelFamily), errors.Is(err, sop.ErrMembershipConflict), errors.Is(err, sop.ErrModelFamilyArchived), errors.Is(err, sop.ErrModelCodeTaken):
		c.JSON(http.StatusConflict, gin.H{"code": "lifecycle_conflict", "message": err.Error()})
	case errors.Is(err, sop.ErrModelFamilyInvalid):
		respondModelFamilyBadRequest(c, err)
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "internal server error"})
	}
}
