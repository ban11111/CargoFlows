package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	_ "image/jpeg"
	"image/png"
	"io"
	"net/http"
	"strings"

	"cargoflows/api/internal/models"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	_ "golang.org/x/image/webp"
	"gorm.io/gorm"
)

type styleReferenceGrantDTO struct {
	PublicID         string `json:"public_id"`
	Version          int    `json:"version"`
	SourceSKUID      string `json:"source_sku_id"`
	DescriptionZH    string `json:"description_zh"`
	DescriptionEN    string `json:"description_en"`
	DerivativeSHA256 string `json:"derivative_sha256"`
	Status           string `json:"status"`
}

func (s *Server) listStyleReferenceGrants(c *gin.Context) {
	var grants []models.StyleReferenceGrant
	if err := s.db.Preload("Asset.SKU").Where("status = ?", "approved").Order("created_at DESC").Find(&grants).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "style references could not be loaded"})
		return
	}
	values := make([]styleReferenceGrantDTO, 0, len(grants))
	for _, grant := range grants {
		values = append(values, styleReferenceGrantDTO{grant.PublicID, grant.Version, grant.Asset.SKU.PublicID, grant.DescriptionZH, grant.DescriptionEN, grant.DerivativeSHA256, grant.Status})
	}
	c.JSON(http.StatusOK, gin.H{"data": values})
}

func (s *Server) createStyleReferenceGrant(c *gin.Context) {
	if !strings.HasPrefix(c.ContentType(), "multipart/form-data") || c.Request.ParseMultipartForm(20<<20) != nil {
		respondAIBadRequest(c, errors.New("multipart asset_id, descriptions, and product_exclusion_mask are required"))
		return
	}
	assetID := strings.TrimSpace(c.PostForm("asset_id"))
	zh := strings.TrimSpace(c.PostForm("description_zh"))
	en := strings.TrimSpace(c.PostForm("description_en"))
	if !isUUID(assetID) || zh == "" || en == "" || len(zh) > 2000 || len(en) > 2000 {
		respondAIBadRequest(c, errors.New("valid asset_id and bilingual style descriptions are required"))
		return
	}
	var asset models.Asset
	if err := s.db.Preload("SKU").Where("public_id = ? AND review_status = ?", assetID, "approved").First(&asset).Error; err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"code": "style_source_not_eligible", "message": "style source must be an approved asset"})
		return
	}
	file, header, err := c.Request.FormFile("product_exclusion_mask")
	if err != nil || header.Size <= 0 || header.Size > 10<<20 {
		respondAIBadRequest(c, errors.New("product_exclusion_mask must be a PNG smaller than 10 MB"))
		return
	}
	defer file.Close()
	maskBytes, err := io.ReadAll(io.LimitReader(file, (10<<20)+1))
	if err != nil || len(maskBytes) > 10<<20 {
		respondAIBadRequest(c, errors.New("product exclusion mask could not be read"))
		return
	}
	mask, err := png.Decode(bytes.NewReader(maskBytes))
	if err != nil {
		respondAIBadRequest(c, errors.New("product exclusion mask must be a valid PNG"))
		return
	}
	source, err := s.storage.ReadSource(c.Request.Context(), asset.ObjectKey)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "object_storage_unavailable", "message": "style source could not be read"})
		return
	}
	original, _, err := image.Decode(bytes.NewReader(source.Bytes))
	if err != nil || original.Bounds().Dx() != mask.Bounds().Dx() || original.Bounds().Dy() != mask.Bounds().Dy() {
		respondAIBadRequest(c, errors.New("product exclusion mask dimensions must match the source image"))
		return
	}
	derivative, changed, protected := neutralizeMaskedProduct(original, mask)
	if !changed || !protected {
		respondAIBadRequest(c, errors.New("product exclusion mask must contain both an excluded product region and retained style pixels"))
		return
	}
	var encoded bytes.Buffer
	if png.Encode(&encoded, derivative) != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "style derivative could not be encoded"})
		return
	}
	digest := sha256.Sum256(encoded.Bytes())
	maskDigest := sha256.Sum256(maskBytes)
	publicID := uuid.NewString()
	maskKey := "reference-grants/style/" + publicID + "/product-mask.png"
	derivativeKey := "reference-grants/style/" + publicID + "/style-derivative.png"
	store, ok := s.storage.(interface {
		StoreReferenceDerivative(context.Context, string, string, []byte) error
	})
	if !ok || store.StoreReferenceDerivative(c.Request.Context(), maskKey, "image/png", maskBytes) != nil || store.StoreReferenceDerivative(c.Request.Context(), derivativeKey, "image/png", encoded.Bytes()) != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "object_storage_unavailable", "message": "private style derivative could not be stored"})
		return
	}
	var previousVersion int
	if s.db.Model(&models.StyleReferenceGrant{}).Where("asset_id = ?", asset.ID).Select("COALESCE(MAX(version), 0)").Scan(&previousVersion).Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "style reference version could not be resolved"})
		return
	}
	grant := models.StyleReferenceGrant{PublicID: publicID, AssetID: asset.ID, Version: previousVersion + 1, DescriptionZH: zh, DescriptionEN: en, ExclusionMaskObjectKey: maskKey, DerivativeObjectKey: derivativeKey, DerivativeSHA256: hex.EncodeToString(digest[:]), Status: "approved", ReviewedByID: currentUser(c).ID}
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&grant).Error; err != nil {
			return err
		}
		actor := currentUser(c).ID
		metadata, _ := json.Marshal(map[string]any{"source_asset_id": asset.PublicID, "source_sku_id": asset.SKU.PublicID, "mask_sha256": hex.EncodeToString(maskDigest[:]), "derivative_sha256": grant.DerivativeSHA256})
		return tx.Create(&models.AIAuditEvent{PublicID: uuid.NewString(), EventType: "style_reference.approved", EntityType: "style_reference_grant", EntityPublicID: grant.PublicID, ActorID: &actor, MetadataJSON: metadata}).Error
	}); errors.Is(err, gorm.ErrDuplicatedKey) {
		c.JSON(http.StatusConflict, gin.H{"code": "style_reference_version_conflict", "message": "style reference changed concurrently; retry with the latest version"})
		return
	} else if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "style reference could not be created"})
		return
	}
	c.JSON(http.StatusCreated, styleReferenceGrantDTO{grant.PublicID, grant.Version, asset.SKU.PublicID, grant.DescriptionZH, grant.DescriptionEN, grant.DerivativeSHA256, grant.Status})
}

func (s *Server) revokeStyleReferenceGrant(c *gin.Context) {
	if !isUUID(c.Param("grant_id")) {
		respondAIBadRequest(c, errors.New("grant_id must be a UUID"))
		return
	}
	var req struct {
		Status string `json:"status"`
	}
	if decodeJSONStrict(c, &req) != nil || req.Status != "revoked" {
		respondAIBadRequest(c, errors.New("status must be revoked"))
		return
	}
	var grant models.StyleReferenceGrant
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("public_id=?", c.Param("grant_id")).First(&grant).Error; err != nil {
			return err
		}
		if err := tx.Model(&grant).Update("status", "revoked").Error; err != nil {
			return err
		}
		actor := currentUser(c).ID
		metadata, _ := json.Marshal(map[string]any{"version": grant.Version})
		return tx.Create(&models.AIAuditEvent{PublicID: uuid.NewString(), EventType: "style_reference.revoked", EntityType: "style_reference_grant", EntityPublicID: grant.PublicID, ActorID: &actor, MetadataJSON: metadata}).Error
	}); errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"code": "not_found", "message": "style reference not found"})
		return
	} else if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "style reference could not be revoked"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"public_id": grant.PublicID, "status": "revoked"})
}

// Transparent/partially transparent mask pixels identify the product region to
// remove. Those pixels are replaced with a neutral, identity-free field; only
// retained pixels can influence later cross-SKU style generation.
func neutralizeMaskedProduct(source image.Image, mask image.Image) (*image.NRGBA, bool, bool) {
	bounds := source.Bounds()
	result := image.NewNRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	minX, minY, maxX, maxY := bounds.Dx(), bounds.Dy(), -1, -1
	for y := 0; y < bounds.Dy(); y++ {
		for x := 0; x < bounds.Dx(); x++ {
			_, _, _, a := mask.At(mask.Bounds().Min.X+x, mask.Bounds().Min.Y+y).RGBA()
			if a < 0xffff {
				minX, minY = min(minX, x), min(minY, y)
				maxX, maxY = max(maxX, x), max(maxY, y)
			}
		}
	}
	if maxX < minX || maxY < minY {
		return result, false, false
	}
	// Redact the full bounding rectangle, not only the mask silhouette. This
	// prevents a tight product mask from leaking the source product outline to
	// another SKU while retaining the surrounding background/style pixels.
	for y := 0; y < bounds.Dy(); y++ {
		for x := 0; x < bounds.Dx(); x++ {
			if x >= minX && x <= maxX && y >= minY && y <= maxY {
				result.SetNRGBA(x, y, color.NRGBA{R: 128, G: 128, B: 128, A: 255})
			} else {
				result.Set(x, y, source.At(bounds.Min.X+x, bounds.Min.Y+y))
			}
		}
	}
	protected := minX > 0 || minY > 0 || maxX < bounds.Dx()-1 || maxY < bounds.Dy()-1
	return result, true, protected
}

type familyReferenceDTO struct {
	PublicID         string `json:"public_id"`
	Version          int    `json:"version"`
	SourceSKUID      string `json:"source_sku_id"`
	Role             string `json:"role"`
	DerivativeSHA256 string `json:"derivative_sha256"`
	Status           string `json:"status"`
}

func (s *Server) listModelFamilyReferenceAssets(c *gin.Context) {
	if !isUUID(c.Param("family_id")) {
		respondAIBadRequest(c, errors.New("family_id must be a UUID"))
		return
	}
	var family models.ModelFamily
	if s.db.Where("public_id = ?", c.Param("family_id")).First(&family).Error != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": "not_found", "message": "model family not found"})
		return
	}
	var values []models.ModelFamilyReferenceAsset
	if s.db.Preload("Asset.SKU").Where("model_family_id = ?", family.ID).Order("created_at DESC").Find(&values).Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "structure references could not be loaded"})
		return
	}
	docs := make([]familyReferenceDTO, 0, len(values))
	for _, v := range values {
		docs = append(docs, familyReferenceDTO{v.PublicID, v.Version, v.Asset.SKU.PublicID, v.Role, v.DerivativeSHA256, v.Status})
	}
	c.JSON(http.StatusOK, gin.H{"data": docs})
}

func (s *Server) createModelFamilyReferenceAsset(c *gin.Context) {
	if !isUUID(c.Param("family_id")) || !strings.HasPrefix(c.ContentType(), "multipart/form-data") || c.Request.ParseMultipartForm(20<<20) != nil {
		respondAIBadRequest(c, errors.New("valid family_id and multipart reference data are required"))
		return
	}
	role := strings.TrimSpace(c.PostForm("role"))
	if role != "geometry_only" && role != "viewpoint_only" && role != "detail_geometry" {
		respondAIBadRequest(c, errors.New("role must be geometry_only, viewpoint_only, or detail_geometry"))
		return
	}
	assetID := strings.TrimSpace(c.PostForm("asset_id"))
	if !isUUID(assetID) {
		respondAIBadRequest(c, errors.New("asset_id must be a UUID"))
		return
	}
	var family models.ModelFamily
	if s.db.Where("public_id=? AND status=?", c.Param("family_id"), models.ModelFamilyActive).First(&family).Error != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": "not_found", "message": "active model family not found"})
		return
	}
	var asset models.Asset
	if s.db.Preload("SKU").Where("public_id=? AND review_status=? AND origin_type<>?", assetID, "approved", "ai_generated").First(&asset).Error != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"code": "structure_source_not_eligible", "message": "structure source must be an approved real asset"})
		return
	}
	var member models.ModelFamilyMember
	if s.db.Where("model_family_id=? AND sk_uid=? AND removed_at IS NULL", family.ID, asset.SKUID).First(&member).Error != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"code": "different_model_family", "message": "structure source must belong to this model family"})
		return
	}
	file, header, err := c.Request.FormFile("forbidden_identity_mask")
	if err != nil || header.Size <= 0 || header.Size > 10<<20 {
		respondAIBadRequest(c, errors.New("forbidden_identity_mask must be a PNG smaller than 10 MB"))
		return
	}
	defer file.Close()
	maskBytes, err := io.ReadAll(io.LimitReader(file, (10<<20)+1))
	if err != nil || len(maskBytes) > 10<<20 {
		respondAIBadRequest(c, errors.New("forbidden identity mask could not be read"))
		return
	}
	mask, err := png.Decode(bytes.NewReader(maskBytes))
	if err != nil {
		respondAIBadRequest(c, errors.New("forbidden identity mask must be a valid PNG"))
		return
	}
	source, err := s.storage.ReadSource(c.Request.Context(), asset.ObjectKey)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "object_storage_unavailable", "message": "structure source could not be read"})
		return
	}
	original, _, err := image.Decode(bytes.NewReader(source.Bytes))
	if err != nil || original.Bounds().Dx() != mask.Bounds().Dx() || original.Bounds().Dy() != mask.Bounds().Dy() {
		respondAIBadRequest(c, errors.New("forbidden identity mask dimensions must match the source image"))
		return
	}
	neutral, changed, protected := neutralizeMaskedProduct(original, mask)
	if !changed || !protected {
		respondAIBadRequest(c, errors.New("mask must cover forbidden identity regions and retain structural pixels"))
		return
	}
	for y := 0; y < neutral.Bounds().Dy(); y++ {
		for x := 0; x < neutral.Bounds().Dx(); x++ {
			gray := color.GrayModel.Convert(neutral.At(x, y)).(color.Gray)
			neutral.Set(x, y, gray)
		}
	}
	var encoded bytes.Buffer
	if png.Encode(&encoded, neutral) != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "structure derivative could not be encoded"})
		return
	}
	digest := sha256.Sum256(encoded.Bytes())
	publicID := uuid.NewString()
	key := "reference-grants/family/" + publicID + "/structure-derivative.png"
	store, ok := s.storage.(interface {
		StoreReferenceDerivative(context.Context, string, string, []byte) error
	})
	if !ok || store.StoreReferenceDerivative(c.Request.Context(), key, "image/png", encoded.Bytes()) != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "object_storage_unavailable", "message": "private structure derivative could not be stored"})
		return
	}
	allowed, _ := json.Marshal([]string{role})
	forbidden, _ := json.Marshal([]string{"color", "material", "labels", "ports", "controls", "accessories", "packaging"})
	var previousVersion int
	if s.db.Model(&models.ModelFamilyReferenceAsset{}).Where("model_family_id = ? AND asset_id = ? AND role = ?", family.ID, asset.ID, role).Select("COALESCE(MAX(version), 0)").Scan(&previousVersion).Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "structure reference version could not be resolved"})
		return
	}
	reference := models.ModelFamilyReferenceAsset{PublicID: publicID, ModelFamilyID: family.ID, AssetID: asset.ID, Role: role, Version: previousVersion + 1, AllowedAttributesJSON: allowed, ForbiddenAttributesJSON: forbidden, DerivativeObjectKey: key, DerivativeSHA256: hex.EncodeToString(digest[:]), Status: "approved", ReviewedByID: currentUser(c).ID}
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&reference).Error; err != nil {
			return err
		}
		actor := currentUser(c).ID
		metadata, _ := json.Marshal(map[string]any{"source_asset_id": asset.PublicID, "source_sku_id": asset.SKU.PublicID, "role": role, "forbidden_attributes": json.RawMessage(forbidden), "derivative_sha256": reference.DerivativeSHA256})
		return tx.Create(&models.AIAuditEvent{PublicID: uuid.NewString(), EventType: "model_family_reference.approved", EntityType: "model_family_reference_asset", EntityPublicID: reference.PublicID, ActorID: &actor, MetadataJSON: metadata}).Error
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "structure reference could not be created"})
		return
	}
	c.JSON(http.StatusCreated, familyReferenceDTO{reference.PublicID, reference.Version, asset.SKU.PublicID, reference.Role, reference.DerivativeSHA256, reference.Status})
}

func (s *Server) revokeModelFamilyReferenceAsset(c *gin.Context) {
	if !isUUID(c.Param("family_id")) || !isUUID(c.Param("reference_id")) {
		respondAIBadRequest(c, errors.New("family_id and reference_id must be UUIDs"))
		return
	}
	var req struct {
		Status string `json:"status"`
	}
	if decodeJSONStrict(c, &req) != nil || req.Status != "revoked" {
		respondAIBadRequest(c, errors.New("status must be revoked"))
		return
	}
	var reference models.ModelFamilyReferenceAsset
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Table("model_family_reference_assets AS reference").Select("reference.*").Joins("JOIN model_families AS family ON family.id=reference.model_family_id").Where("family.public_id=? AND reference.public_id=?", c.Param("family_id"), c.Param("reference_id")).First(&reference).Error; err != nil {
			return err
		}
		if err := tx.Model(&reference).Update("status", "revoked").Error; err != nil {
			return err
		}
		actor := currentUser(c).ID
		metadata, _ := json.Marshal(map[string]any{"version": reference.Version, "role": reference.Role})
		return tx.Create(&models.AIAuditEvent{PublicID: uuid.NewString(), EventType: "model_family_reference.revoked", EntityType: "model_family_reference_asset", EntityPublicID: reference.PublicID, ActorID: &actor, MetadataJSON: metadata}).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"code": "not_found", "message": "structure reference not found"})
		return
	} else if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "structure reference could not be revoked"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"public_id": reference.PublicID, "status": "revoked"})
}
