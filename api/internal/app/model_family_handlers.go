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

type variantIdentityVersionMutationRequest struct {
	Identity        json.RawMessage                    `json:"identity"`
	Regions         []sop.VariantDifferenceRegionInput `json:"regions"`
	SourceVersionID *string                            `json:"source_version_id"`
}

type variantIdentityRegionDTO struct {
	PublicID             string                            `json:"public_id"`
	Key                  string                            `json:"key"`
	DifferenceKind       models.DifferenceKind             `json:"difference_kind"`
	Strictness           models.DifferenceRegionStrictness `json:"strictness"`
	DescriptionZH        string                            `json:"description_zh"`
	DescriptionEN        string                            `json:"description_en"`
	Shape                json.RawMessage                   `json:"shape"`
	ForbiddenInheritance json.RawMessage                   `json:"forbidden_inheritance"`
	RequiredViewKeys     json.RawMessage                   `json:"required_view_keys"`
	EvidenceAssetIDs     []string                          `json:"evidence_asset_ids"`
}

type variantIdentityVersionDTO struct {
	PublicID      string                       `json:"public_id"`
	SKUId         string                       `json:"sku_id"`
	VersionNumber int                          `json:"version_number"`
	Status        models.VariantManifestStatus `json:"status"`
	Identity      json.RawMessage              `json:"identity"`
	PublishedAt   *time.Time                   `json:"published_at"`
	CreatedAt     time.Time                    `json:"created_at"`
	Regions       []variantIdentityRegionDTO   `json:"regions"`
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

func (s *Server) getSKUVariantIdentity(c *gin.Context) {
	if !requireUUIDParam(c, "sku_id") {
		return
	}
	version, err := sop.NewVariantManifestService(s.db).GetForSKU(c.Request.Context(), c.Param("sku_id"))
	if err != nil {
		respondVariantManifestError(c, err)
		return
	}
	dto, err := s.variantIdentityVersionDTO(*version)
	if err != nil {
		respondVariantManifestError(c, err)
		return
	}
	c.JSON(http.StatusOK, dto)
}

func (s *Server) createSKUVariantIdentityVersion(c *gin.Context) {
	if !requireUUIDParam(c, "sku_id") {
		return
	}
	var req variantIdentityVersionMutationRequest
	if err := decodeJSONStrict(c, &req); err != nil {
		respondModelFamilyBadRequest(c, err)
		return
	}
	service := sop.NewVariantManifestService(s.db)
	var version *models.VariantIdentityManifestVersion
	var err error
	if req.SourceVersionID != nil {
		if !isUUID(*req.SourceVersionID) || req.Identity != nil || req.Regions != nil {
			respondModelFamilyBadRequest(c, errors.New("source_version_id is mutually exclusive with identity and regions"))
			return
		}
		version, err = service.CopyVersion(c.Request.Context(), c.Param("sku_id"), *req.SourceVersionID, currentUser(c).ID)
	} else {
		if req.Identity == nil || req.Regions == nil {
			respondModelFamilyBadRequest(c, errors.New("identity and regions are required"))
			return
		}
		version, err = service.CreateDraft(c.Request.Context(), c.Param("sku_id"), sop.CreateVariantManifestDraftInput{Identity: req.Identity, Regions: req.Regions, ActorID: currentUser(c).ID})
	}
	if err != nil {
		respondVariantManifestError(c, err)
		return
	}
	dto, err := s.variantIdentityVersionDTO(*version)
	if err != nil {
		respondVariantManifestError(c, err)
		return
	}
	c.JSON(http.StatusCreated, dto)
}

func (s *Server) updateVariantIdentityVersion(c *gin.Context) {
	if !requireUUIDParam(c, "version_id") {
		return
	}
	var req variantIdentityVersionMutationRequest
	if err := decodeJSONStrict(c, &req); err != nil {
		respondModelFamilyBadRequest(c, err)
		return
	}
	if req.SourceVersionID != nil || req.Identity == nil || req.Regions == nil {
		respondModelFamilyBadRequest(c, errors.New("identity and regions are required and source_version_id is not allowed"))
		return
	}
	version, err := sop.NewVariantManifestService(s.db).UpdateDraft(c.Request.Context(), c.Param("version_id"), sop.UpdateVariantManifestDraftInput{Identity: req.Identity, Regions: req.Regions, ActorID: currentUser(c).ID})
	if err != nil {
		respondVariantManifestError(c, err)
		return
	}
	dto, err := s.variantIdentityVersionDTO(*version)
	if err != nil {
		respondVariantManifestError(c, err)
		return
	}
	c.JSON(http.StatusOK, dto)
}

func (s *Server) validateVariantIdentityVersion(c *gin.Context) {
	if !requireUUIDParam(c, "version_id") {
		return
	}
	issues, err := sop.NewVariantManifestService(s.db).Validate(c.Request.Context(), c.Param("version_id"))
	if err != nil {
		respondVariantManifestError(c, err)
		return
	}
	code := "variant_manifest_valid"
	if len(issues) != 0 {
		code = "variant_manifest_validation_failed"
	}
	c.JSON(http.StatusOK, gin.H{"code": code, "errors": issues})
}

func (s *Server) publishVariantIdentityVersion(c *gin.Context) {
	if !requireUUIDParam(c, "version_id") {
		return
	}
	version, err := sop.NewVariantManifestService(s.db).Publish(c.Request.Context(), c.Param("version_id"), currentUser(c).ID)
	if err != nil {
		respondVariantManifestError(c, err)
		return
	}
	dto, err := s.variantIdentityVersionDTO(*version)
	if err != nil {
		respondVariantManifestError(c, err)
		return
	}
	c.JSON(http.StatusOK, dto)
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

func (s *Server) variantIdentityVersionDTO(version models.VariantIdentityManifestVersion) (variantIdentityVersionDTO, error) {
	regionAssetIDs := make(map[uint][]string, len(version.Regions))
	if len(version.Regions) != 0 {
		regionIDs := make([]uint, 0, len(version.Regions))
		for _, region := range version.Regions {
			regionIDs = append(regionIDs, region.ID)
		}
		var rows []struct {
			RegionID uint
			PublicID string
		}
		if err := s.db.Table("variant_difference_region_evidence_assets").Select("variant_difference_region_evidence_assets.variant_difference_region_id AS region_id, assets.public_id").Joins("JOIN assets ON assets.id = variant_difference_region_evidence_assets.asset_id").Where("variant_difference_region_evidence_assets.variant_difference_region_id IN ?", regionIDs).Order("variant_difference_region_evidence_assets.id ASC").Scan(&rows).Error; err != nil {
			return variantIdentityVersionDTO{}, err
		}
		for _, row := range rows {
			regionAssetIDs[row.RegionID] = append(regionAssetIDs[row.RegionID], row.PublicID)
		}
	}
	var manifest models.VariantIdentityManifest
	if err := s.db.Select("sk_uid").First(&manifest, version.VariantIdentityManifestID).Error; err != nil {
		return variantIdentityVersionDTO{}, err
	}
	var sku models.SKU
	if err := s.db.Select("public_id").First(&sku, manifest.SKUID).Error; err != nil {
		return variantIdentityVersionDTO{}, err
	}
	dto := variantIdentityVersionDTO{PublicID: version.PublicID, SKUId: sku.PublicID, VersionNumber: version.VersionNumber, Status: version.Status, Identity: append(json.RawMessage(nil), version.IdentityJSON...), PublishedAt: version.PublishedAt, CreatedAt: version.CreatedAt, Regions: make([]variantIdentityRegionDTO, 0, len(version.Regions))}
	for _, region := range version.Regions {
		evidenceAssetIDs := regionAssetIDs[region.ID]
		if evidenceAssetIDs == nil {
			evidenceAssetIDs = []string{}
		}
		dto.Regions = append(dto.Regions, variantIdentityRegionDTO{PublicID: region.PublicID, Key: region.Key, DifferenceKind: region.DifferenceKind, Strictness: region.Strictness, DescriptionZH: region.DescriptionZH, DescriptionEN: region.DescriptionEN, Shape: append(json.RawMessage(nil), region.ShapeJSON...), ForbiddenInheritance: append(json.RawMessage(nil), region.ForbiddenInheritanceJSON...), RequiredViewKeys: append(json.RawMessage(nil), region.RequiredViewKeysJSON...), EvidenceAssetIDs: evidenceAssetIDs})
	}
	return dto, nil
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

func respondVariantManifestError(c *gin.Context, err error) {
	var validation *sop.VariantManifestValidationError
	switch {
	case errors.As(err, &validation):
		c.JSON(http.StatusUnprocessableEntity, gin.H{"code": "variant_manifest_validation_failed", "errors": validation.Issues})
	case errors.Is(err, sop.ErrVariantManifestNotFound), errors.Is(err, sop.ErrVariantManifestVersionNotFound), errors.Is(err, sop.ErrSKUNotFound):
		c.JSON(http.StatusNotFound, gin.H{"code": "not_found", "message": "resource not found"})
	case errors.Is(err, sop.ErrVariantManifestDraftExists), errors.Is(err, sop.ErrVariantManifestImmutable), errors.Is(err, sop.ErrVariantManifestSourceNotPublished), errors.Is(err, sop.ErrModelFamilyArchived):
		c.JSON(http.StatusConflict, gin.H{"code": "version_immutable", "message": err.Error()})
	case errors.Is(err, sop.ErrVariantManifestInvalid):
		respondModelFamilyBadRequest(c, err)
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "internal server error"})
	}
}
