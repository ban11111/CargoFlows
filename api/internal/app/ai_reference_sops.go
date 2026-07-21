package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image"
	_ "image/jpeg"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"cargoflows/api/internal/ai"
	"cargoflows/api/internal/models"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type aiReferenceSOPMutation struct {
	CategoryID    uint   `json:"category_id"`
	NameZH        string `json:"name_zh"`
	NameEN        string `json:"name_en"`
	DescriptionZH string `json:"description_zh"`
	DescriptionEN string `json:"description_en"`
}

type aiReferenceVersionMutation struct {
	NameZH        string `json:"name_zh"`
	NameEN        string `json:"name_en"`
	DescriptionZH string `json:"description_zh"`
	DescriptionEN string `json:"description_en"`
}

func validReferencePurpose(value models.AIReferencePurpose) bool {
	return value == models.AIReferenceVisualStyle || value == models.AIReferenceUsageEffect || value == models.AIReferenceCopyInspiration
}

func (s *Server) listAIReferenceSOPs(c *gin.Context) {
	var values []models.AIReferenceSOP
	query := s.db.Preload("Category").Preload("Versions", func(db *gorm.DB) *gorm.DB { return db.Order("version_number DESC") }).Preload("Versions.Items", func(db *gorm.DB) *gorm.DB { return db.Order("sort_order ASC") }).Order("updated_at DESC")
	if raw := strings.TrimSpace(c.Query("category_id")); raw != "" {
		categoryID, err := parseOptionalUint(raw)
		if err != nil || categoryID == 0 {
			respondAIBadRequest(c, errors.New("category_id must be a positive integer"))
			return
		}
		query = query.Where("category_id = ?", categoryID)
	}
	includeAll := c.Query("include_all") == "true"
	if includeAll && !isAdministrator(currentUser(c)) {
		c.JSON(http.StatusForbidden, gin.H{"code": "forbidden", "message": "administrator access required"})
		return
	}
	if err := query.Find(&values).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "AI reference SOPs could not be loaded"})
		return
	}
	if !includeAll {
		filtered := values[:0]
		for index := range values {
			published := values[index].Versions[:0]
			for _, version := range values[index].Versions {
				if version.Status == models.SOPVersionPublished {
					published = append(published, version)
				}
			}
			values[index].Versions = published
			if len(published) > 0 {
				filtered = append(filtered, values[index])
			}
		}
		values = filtered
	}
	c.JSON(http.StatusOK, gin.H{"data": values})
}

func (s *Server) getAIReferenceSOP(c *gin.Context) {
	var value models.AIReferenceSOP
	if !requireUUIDParam(c, "sop_id") {
		return
	}
	if err := s.db.Preload("Category").Preload("Versions", func(db *gorm.DB) *gorm.DB { return db.Order("version_number DESC") }).Preload("Versions.Items", func(db *gorm.DB) *gorm.DB { return db.Order("sort_order ASC") }).Where("public_id = ?", c.Param("sop_id")).First(&value).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": "not_found", "message": "AI reference SOP not found"})
		return
	}
	if !isAdministrator(currentUser(c)) {
		published := value.Versions[:0]
		for _, version := range value.Versions {
			if version.Status == models.SOPVersionPublished {
				published = append(published, version)
			}
		}
		if len(published) == 0 {
			c.JSON(http.StatusNotFound, gin.H{"code": "not_found", "message": "AI reference SOP not found"})
			return
		}
		value.Versions = published
	}
	c.JSON(http.StatusOK, value)
}

func (s *Server) createAIReferenceSOP(c *gin.Context) {
	var req aiReferenceSOPMutation
	if decodeJSONStrict(c, &req) != nil || req.CategoryID == 0 || strings.TrimSpace(req.NameZH) == "" || strings.TrimSpace(req.NameEN) == "" || strings.TrimSpace(req.DescriptionZH) == "" || strings.TrimSpace(req.DescriptionEN) == "" {
		respondAIBadRequest(c, errors.New("category_id and bilingual names and descriptions are required"))
		return
	}
	var category models.Category
	if err := s.db.First(&category, req.CategoryID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": "not_found", "message": "category not found"})
		return
	}
	value := models.AIReferenceSOP{PublicID: uuid.NewString(), CategoryID: req.CategoryID, CreatedByID: currentUser(c).ID}
	version := models.AIReferenceSOPVersion{PublicID: uuid.NewString(), VersionNumber: 1, NameZH: strings.TrimSpace(req.NameZH), NameEN: strings.TrimSpace(req.NameEN), DescriptionZH: strings.TrimSpace(req.DescriptionZH), DescriptionEN: strings.TrimSpace(req.DescriptionEN), Status: models.SOPVersionDraft}
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&value).Error; err != nil {
			return err
		}
		version.AIReferenceSOPID = value.ID
		return tx.Create(&version).Error
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "AI reference SOP could not be created"})
		return
	}
	value.Category, value.Versions = category, []models.AIReferenceSOPVersion{version}
	c.JSON(http.StatusCreated, value)
}

func (s *Server) updateAIReferenceSOPVersion(c *gin.Context) {
	var req aiReferenceVersionMutation
	if !requireUUIDParam(c, "version_id") {
		return
	}
	if decodeJSONStrict(c, &req) != nil || strings.TrimSpace(req.NameZH) == "" || strings.TrimSpace(req.NameEN) == "" || strings.TrimSpace(req.DescriptionZH) == "" || strings.TrimSpace(req.DescriptionEN) == "" {
		respondAIBadRequest(c, errors.New("bilingual names and descriptions are required"))
		return
	}
	updates := map[string]any{"name_zh": strings.TrimSpace(req.NameZH), "name_en": strings.TrimSpace(req.NameEN), "description_zh": strings.TrimSpace(req.DescriptionZH), "description_en": strings.TrimSpace(req.DescriptionEN)}
	result := s.db.Model(&models.AIReferenceSOPVersion{}).Where("public_id = ? AND status = ?", c.Param("version_id"), models.SOPVersionDraft).Updates(updates)
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "version could not be updated"})
		return
	}
	if result.RowsAffected != 1 {
		c.JSON(http.StatusConflict, gin.H{"code": "immutable_version", "message": "only draft versions can be edited"})
		return
	}
	s.getAIReferenceVersion(c)
}

func (s *Server) getAIReferenceVersion(c *gin.Context) {
	var value models.AIReferenceSOPVersion
	if err := s.db.Preload("Items", func(db *gorm.DB) *gorm.DB { return db.Order("sort_order ASC") }).Where("public_id = ?", c.Param("version_id")).First(&value).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": "not_found", "message": "version not found"})
		return
	}
	c.JSON(http.StatusOK, value)
}

func (s *Server) copyAIReferenceSOPVersion(c *gin.Context) {
	if !requireUUIDParam(c, "sop_id") {
		return
	}
	var req struct {
		SourceVersionID string `json:"source_version_id"`
	}
	if decodeJSONStrict(c, &req) != nil || !isUUID(req.SourceVersionID) {
		respondAIBadRequest(c, errors.New("source_version_id is required"))
		return
	}
	var created models.AIReferenceSOPVersion
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var sop models.AIReferenceSOP
		if err := tx.Where("public_id = ?", c.Param("sop_id")).First(&sop).Error; err != nil {
			return err
		}
		var source models.AIReferenceSOPVersion
		if err := tx.Preload("Items", func(db *gorm.DB) *gorm.DB { return db.Order("sort_order") }).Where("public_id = ? AND ai_reference_sop_id = ?", req.SourceVersionID, sop.ID).First(&source).Error; err != nil {
			return err
		}
		var draftCount int64
		if err := tx.Model(&models.AIReferenceSOPVersion{}).Where("ai_reference_sop_id = ? AND status = ?", sop.ID, models.SOPVersionDraft).Count(&draftCount).Error; err != nil {
			return err
		}
		if draftCount > 0 {
			return errors.New("draft_exists")
		}
		var maximum int
		if err := tx.Model(&models.AIReferenceSOPVersion{}).Where("ai_reference_sop_id = ?", sop.ID).Select("COALESCE(MAX(version_number),0)").Scan(&maximum).Error; err != nil {
			return err
		}
		created = models.AIReferenceSOPVersion{PublicID: uuid.NewString(), AIReferenceSOPID: sop.ID, VersionNumber: maximum + 1, NameZH: source.NameZH, NameEN: source.NameEN, DescriptionZH: source.DescriptionZH, DescriptionEN: source.DescriptionEN, Status: models.SOPVersionDraft, CopiedFromVersionID: &source.ID}
		if err := tx.Create(&created).Error; err != nil {
			return err
		}
		for _, item := range source.Items {
			item.ID = 0
			item.PublicID = uuid.NewString()
			item.AIReferenceSOPVersionID = created.ID
			item.CreatedAt = time.Time{}
			item.UpdatedAt = time.Time{}
			if err := tx.Create(&item).Error; err != nil {
				return err
			}
		}
		return tx.Preload("Items", func(db *gorm.DB) *gorm.DB { return db.Order("sort_order") }).First(&created, created.ID).Error
	})
	if err != nil {
		if err.Error() == "draft_exists" {
			c.JSON(http.StatusConflict, gin.H{"code": "draft_exists", "message": "archive or publish the existing draft first"})
		} else {
			c.JSON(http.StatusUnprocessableEntity, gin.H{"code": "copy_failed", "message": "version could not be copied"})
		}
		return
	}
	c.JSON(http.StatusCreated, created)
}

func readMultipartImage(c *gin.Context, field string, required bool) ([]byte, image.Image, string, error) {
	file, header, err := c.Request.FormFile(field)
	if err != nil {
		if !required {
			return nil, nil, "", nil
		}
		return nil, nil, "", err
	}
	defer file.Close()
	if header.Size <= 0 || header.Size > 10<<20 {
		return nil, nil, "", errors.New("image must be smaller than 10 MB")
	}
	value, err := io.ReadAll(io.LimitReader(file, (10<<20)+1))
	if err != nil || len(value) > 10<<20 {
		return nil, nil, "", errors.New("image could not be read")
	}
	decoded, format, err := image.Decode(bytes.NewReader(value))
	if err != nil || decoded.Bounds().Dx() <= 0 || decoded.Bounds().Dy() <= 0 || int64(decoded.Bounds().Dx())*int64(decoded.Bounds().Dy()) > 40_000_000 {
		return nil, nil, "", errors.New("invalid or oversized image")
	}
	if format != "jpeg" && format != "png" && format != "webp" {
		return nil, nil, "", errors.New("image must be JPEG, PNG, or WebP")
	}
	return value, decoded, format, nil
}

type aiReferenceItemCompletion struct {
	CompletionToken     string `json:"completion_token"`
	CaptionZH           string `json:"caption_zh"`
	CaptionEN           string `json:"caption_en"`
	AllowedGuidanceZH   string `json:"allowed_guidance_zh"`
	AllowedGuidanceEN   string `json:"allowed_guidance_en"`
	ForbiddenGuidanceZH string `json:"forbidden_guidance_zh"`
	ForbiddenGuidanceEN string `json:"forbidden_guidance_en"`
	SourceName          string `json:"source_name"`
	SourceURL           string `json:"source_url"`
	RightsConfirmed     bool   `json:"rights_confirmed"`
}

func (s *Server) createAIReferenceItemUploadURL(c *gin.Context) {
	if !requireUUIDParam(c, "version_id") {
		return
	}
	var req struct {
		Purpose         models.AIReferencePurpose `json:"purpose"`
		FileName        string                    `json:"file_name"`
		ContentType     string                    `json:"content_type"`
		MaskFileName    string                    `json:"mask_file_name"`
		MaskContentType string                    `json:"mask_content_type"`
	}
	if decodeJSONStrict(c, &req) != nil || !validReferencePurpose(req.Purpose) {
		respondAIBadRequest(c, errors.New("valid purpose and image metadata are required"))
		return
	}
	contentType := normalizedImageContentType(req.ContentType)
	extension, supported := imageExtension(contentType)
	if !supported || (contentType != "image/jpeg" && contentType != "image/png" && contentType != "image/webp") {
		respondAIBadRequest(c, errors.New("image must be JPEG, PNG, or WebP"))
		return
	}
	if strings.TrimSpace(req.FileName) == "" {
		respondAIBadRequest(c, errors.New("file_name is required"))
		return
	}
	maskKey := ""
	if req.Purpose == models.AIReferenceVisualStyle {
		if normalizedImageContentType(req.MaskContentType) != "image/png" || strings.TrimSpace(req.MaskFileName) == "" {
			respondAIBadRequest(c, errors.New("visual_style requires a PNG product exclusion mask"))
			return
		}
	}
	var version models.AIReferenceSOPVersion
	if err := s.db.Where("public_id=? AND status=?", c.Param("version_id"), models.SOPVersionDraft).First(&version).Error; err != nil {
		c.JSON(http.StatusConflict, gin.H{"code": "immutable_version", "message": "only draft versions can accept uploads"})
		return
	}
	ticketID := uuid.NewString()
	temporaryKey := "ai-reference-sop-uploads/" + ticketID + extension
	if req.Purpose == models.AIReferenceVisualStyle {
		maskKey = "ai-reference-sop-uploads/" + ticketID + "-mask.png"
	}
	upload := models.AIReferenceUpload{PublicID: ticketID, AIReferenceSOPVersionID: version.ID, CreatedByID: currentUser(c).ID, Purpose: req.Purpose, TemporaryKey: temporaryKey, ContentType: contentType, MaskTemporaryKey: maskKey, MaskContentType: normalizedImageContentType(req.MaskContentType), ExpiresAt: time.Now().Add(15 * time.Minute)}
	if err := s.db.Create(&upload).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "upload ticket could not be created"})
		return
	}
	uploadURL, _, err := s.storage.createUploadURL(c.Request.Context(), temporaryKey)
	if err != nil {
		_ = s.db.Delete(&upload).Error
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "object_storage_unavailable", "message": "upload URL could not be created"})
		return
	}
	response := gin.H{"completion_token": ticketID, "expires_in": 900, "image": gin.H{"method": "PUT", "upload_url": uploadURL, "headers": gin.H{"content-type": contentType}}}
	if maskKey != "" {
		maskURL, _, maskErr := s.storage.createUploadURL(c.Request.Context(), maskKey)
		if maskErr != nil {
			_ = s.db.Delete(&upload).Error
			c.JSON(http.StatusServiceUnavailable, gin.H{"code": "object_storage_unavailable", "message": "mask upload URL could not be created"})
			return
		}
		response["mask"] = gin.H{"method": "PUT", "upload_url": maskURL, "headers": gin.H{"content-type": "image/png"}}
	}
	c.JSON(http.StatusOK, response)
}

func (s *Server) completeAIReferenceItemUpload(c *gin.Context) {
	if !requireUUIDParam(c, "version_id") {
		return
	}
	var req aiReferenceItemCompletion
	if decodeJSONStrict(c, &req) != nil || !isUUID(req.CompletionToken) {
		respondAIBadRequest(c, errors.New("valid completion_token is required"))
		return
	}
	trimReferenceCompletion(&req)
	if req.CaptionZH == "" || req.CaptionEN == "" || req.AllowedGuidanceZH == "" || req.AllowedGuidanceEN == "" || req.ForbiddenGuidanceZH == "" || req.ForbiddenGuidanceEN == "" || req.SourceName == "" || !req.RightsConfirmed {
		respondAIBadRequest(c, errors.New("bilingual guidance, source_name, and rights confirmation are required"))
		return
	}
	if req.SourceURL != "" {
		parsed, err := url.ParseRequestURI(req.SourceURL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			respondAIBadRequest(c, errors.New("source_url must be an HTTP(S) URL"))
			return
		}
	}
	var version models.AIReferenceSOPVersion
	if err := s.db.Where("public_id=? AND status=?", c.Param("version_id"), models.SOPVersionDraft).First(&version).Error; err != nil {
		c.JSON(http.StatusConflict, gin.H{"code": "immutable_version", "message": "only draft versions can accept items"})
		return
	}
	var upload models.AIReferenceUpload
	if err := s.db.Where("public_id=? AND ai_reference_sop_version_id=? AND created_by_id=? AND consumed_at IS NULL", req.CompletionToken, version.ID, currentUser(c).ID).First(&upload).Error; err != nil || upload.ExpiresAt.Before(time.Now()) {
		respondAIBadRequest(c, errors.New("upload ticket is invalid, expired, or already used"))
		return
	}
	sourceInput, err := s.storage.ReadSource(c.Request.Context(), upload.TemporaryKey)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "object_storage_unavailable", "message": "uploaded image could not be read"})
		return
	}
	metadata, err := new(ai.ImageStorage).Validate(ai.ImageValidationRequest{Bytes: sourceInput.Bytes, MaxBytes: 10 << 20, MaxPixels: 40_000_000})
	if err != nil || metadata.MIMEType != upload.ContentType {
		respondAIBadRequest(c, errors.New("uploaded image is invalid or does not match its declared type"))
		return
	}
	_, source, _, err := readImageBytes(sourceInput.Bytes)
	if err != nil {
		respondAIBadRequest(c, err)
		return
	}
	processed := source
	var maskBytes []byte
	if upload.Purpose == models.AIReferenceVisualStyle {
		maskInput, maskErr := s.storage.ReadSource(c.Request.Context(), upload.MaskTemporaryKey)
		if maskErr != nil {
			respondAIBadRequest(c, errors.New("product exclusion mask was not uploaded"))
			return
		}
		maskBytes = maskInput.Bytes
		_, mask, maskFormat, maskErr := readImageBytes(maskBytes)
		if maskErr != nil || maskFormat != "png" || mask.Bounds() != source.Bounds() {
			respondAIBadRequest(c, errors.New("a same-size PNG product exclusion mask is required"))
			return
		}
		derived, changed, protected := neutralizeMaskedProduct(source, mask)
		if !changed || !protected {
			respondAIBadRequest(c, errors.New("mask must exclude the product and retain style pixels"))
			return
		}
		processed = derived
	}
	var encoded bytes.Buffer
	if png.Encode(&encoded, processed) != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "image could not be normalized"})
		return
	}
	now := time.Now()
	claim := s.db.Model(&models.AIReferenceUpload{}).Where("id=? AND consumed_at IS NULL", upload.ID).Update("consumed_at", now)
	if claim.Error != nil || claim.RowsAffected != 1 {
		respondAIBadRequest(c, errors.New("upload ticket is already used"))
		return
	}
	itemID := uuid.NewString()
	objectKey := "ai-reference-sops/items/" + itemID + "/reference.png"
	thumbnailKey := "ai-reference-sops/items/" + itemID + "/thumbnail.png"
	maskKey := ""
	store, ok := s.storage.(interface {
		StoreReferenceDerivative(context.Context, string, string, []byte) error
	})
	if !ok || store.StoreReferenceDerivative(c.Request.Context(), objectKey, "image/png", encoded.Bytes()) != nil {
		_ = s.db.Model(&models.AIReferenceUpload{}).Where("id=?", upload.ID).Update("consumed_at", nil).Error
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "object_storage_unavailable", "message": "reference image could not be stored"})
		return
	}
	thumbnail, thumbnailErr := referenceThumbnail(processed)
	if thumbnailErr != nil || store.StoreReferenceDerivative(c.Request.Context(), thumbnailKey, "image/png", thumbnail) != nil {
		_ = s.storage.deleteSource(c.Request.Context(), objectKey)
		_ = s.db.Model(&models.AIReferenceUpload{}).Where("id=?", upload.ID).Update("consumed_at", nil).Error
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "object_storage_unavailable", "message": "reference thumbnail could not be stored"})
		return
	}
	if len(maskBytes) > 0 {
		maskKey = "ai-reference-sops/items/" + itemID + "/mask.png"
		if store.StoreReferenceDerivative(c.Request.Context(), maskKey, "image/png", maskBytes) != nil {
			_ = s.storage.deleteSource(c.Request.Context(), objectKey)
			_ = s.db.Model(&models.AIReferenceUpload{}).Where("id=?", upload.ID).Update("consumed_at", nil).Error
			c.JSON(http.StatusServiceUnavailable, gin.H{"code": "object_storage_unavailable", "message": "reference mask could not be stored"})
			return
		}
	}
	digest := sha256.Sum256(encoded.Bytes())
	var item models.AIReferenceItem
	err = s.db.Transaction(func(tx *gorm.DB) error {
		var locked models.AIReferenceSOPVersion
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND status=?", version.ID, models.SOPVersionDraft).First(&locked).Error; err != nil {
			return err
		}
		var maximum int
		if err := tx.Model(&models.AIReferenceItem{}).Where("ai_reference_sop_version_id=?", version.ID).Select("COALESCE(MAX(sort_order),0)").Scan(&maximum).Error; err != nil {
			return err
		}
		item = models.AIReferenceItem{PublicID: itemID, AIReferenceSOPVersionID: version.ID, SortOrder: maximum + 1, Purpose: upload.Purpose, CaptionZH: req.CaptionZH, CaptionEN: req.CaptionEN, AllowedGuidanceZH: req.AllowedGuidanceZH, AllowedGuidanceEN: req.AllowedGuidanceEN, ForbiddenGuidanceZH: req.ForbiddenGuidanceZH, ForbiddenGuidanceEN: req.ForbiddenGuidanceEN, SourceName: req.SourceName, SourceURL: req.SourceURL, ObjectKey: objectKey, ThumbnailObjectKey: thumbnailKey, MaskObjectKey: maskKey, MIMEType: "image/png", Width: processed.Bounds().Dx(), Height: processed.Bounds().Dy(), ByteCount: int64(encoded.Len()), SHA256: hex.EncodeToString(digest[:]), RightsConfirmed: true, CreatedByID: currentUser(c).ID}
		return tx.Create(&item).Error
	})
	if err != nil {
		_ = s.storage.deleteSource(c.Request.Context(), objectKey)
		_ = s.storage.deleteSource(c.Request.Context(), thumbnailKey)
		if maskKey != "" {
			_ = s.storage.deleteSource(c.Request.Context(), maskKey)
		}
		_ = s.db.Model(&models.AIReferenceUpload{}).Where("id=?", upload.ID).Update("consumed_at", nil).Error
		c.JSON(http.StatusConflict, gin.H{"code": "immutable_version", "message": "only draft versions can accept items"})
		return
	}
	_ = s.storage.deleteSource(c.Request.Context(), upload.TemporaryKey)
	if upload.MaskTemporaryKey != "" {
		_ = s.storage.deleteSource(c.Request.Context(), upload.MaskTemporaryKey)
	}
	c.JSON(http.StatusCreated, item)
}

func trimReferenceCompletion(value *aiReferenceItemCompletion) {
	value.CaptionZH, value.CaptionEN = strings.TrimSpace(value.CaptionZH), strings.TrimSpace(value.CaptionEN)
	value.AllowedGuidanceZH, value.AllowedGuidanceEN = strings.TrimSpace(value.AllowedGuidanceZH), strings.TrimSpace(value.AllowedGuidanceEN)
	value.ForbiddenGuidanceZH, value.ForbiddenGuidanceEN = strings.TrimSpace(value.ForbiddenGuidanceZH), strings.TrimSpace(value.ForbiddenGuidanceEN)
	value.SourceName, value.SourceURL = strings.TrimSpace(value.SourceName), strings.TrimSpace(value.SourceURL)
}

func readImageBytes(value []byte) ([]byte, image.Image, string, error) {
	if len(value) == 0 || len(value) > 10<<20 {
		return nil, nil, "", errors.New("image must be smaller than 10 MB")
	}
	decoded, format, err := image.Decode(bytes.NewReader(value))
	if err != nil || decoded.Bounds().Dx() <= 0 || decoded.Bounds().Dy() <= 0 || int64(decoded.Bounds().Dx())*int64(decoded.Bounds().Dy()) > 40_000_000 {
		return nil, nil, "", errors.New("invalid or oversized image")
	}
	return value, decoded, format, nil
}

func referenceThumbnail(source image.Image) ([]byte, error) {
	bounds := source.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	const maximum = 640
	if width > maximum || height > maximum {
		if width >= height {
			height = max(1, height*maximum/width)
			width = maximum
		} else {
			width = max(1, width*maximum/height)
			height = maximum
		}
	}
	thumbnail := image.NewNRGBA(image.Rect(0, 0, width, height))
	xdraw.CatmullRom.Scale(thumbnail, thumbnail.Bounds(), source, bounds, xdraw.Over, nil)
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, thumbnail); err != nil {
		return nil, err
	}
	return encoded.Bytes(), nil
}

func (s *Server) addAIReferenceItem(c *gin.Context) {
	if !requireUUIDParam(c, "version_id") {
		return
	}
	if !strings.HasPrefix(c.ContentType(), "multipart/form-data") || c.Request.ParseMultipartForm(24<<20) != nil {
		respondAIBadRequest(c, errors.New("multipart form is required"))
		return
	}
	purpose := models.AIReferencePurpose(strings.TrimSpace(c.PostForm("purpose")))
	captionZH, captionEN, sourceName := strings.TrimSpace(c.PostForm("caption_zh")), strings.TrimSpace(c.PostForm("caption_en")), strings.TrimSpace(c.PostForm("source_name"))
	allowedZH, allowedEN := strings.TrimSpace(c.PostForm("allowed_guidance_zh")), strings.TrimSpace(c.PostForm("allowed_guidance_en"))
	forbiddenZH, forbiddenEN := strings.TrimSpace(c.PostForm("forbidden_guidance_zh")), strings.TrimSpace(c.PostForm("forbidden_guidance_en"))
	rights := c.PostForm("rights_confirmed") == "true"
	sourceURL := strings.TrimSpace(c.PostForm("source_url"))
	if sourceURL != "" {
		parsed, err := url.ParseRequestURI(sourceURL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			respondAIBadRequest(c, errors.New("source_url must be an HTTP(S) URL"))
			return
		}
	}
	if !validReferencePurpose(purpose) || captionZH == "" || captionEN == "" || sourceName == "" || allowedZH == "" || allowedEN == "" || forbiddenZH == "" || forbiddenEN == "" || !rights {
		respondAIBadRequest(c, errors.New("purpose, bilingual guidance, source_name, and rights confirmation are required"))
		return
	}
	_, source, _, err := readMultipartImage(c, "image", true)
	if err != nil {
		respondAIBadRequest(c, err)
		return
	}
	processed := source
	var maskBytes []byte
	if purpose == models.AIReferenceVisualStyle {
		var mask image.Image
		var maskFormat string
		maskBytes, mask, maskFormat, err = readMultipartImage(c, "product_exclusion_mask", true)
		if err != nil || maskFormat != "png" || mask.Bounds() != source.Bounds() {
			respondAIBadRequest(c, errors.New("a same-size PNG product exclusion mask is required"))
			return
		}
		derived, changed, protected := neutralizeMaskedProduct(source, mask)
		if !changed || !protected {
			respondAIBadRequest(c, errors.New("mask must exclude the product and retain style pixels"))
			return
		}
		processed = derived
	}
	var encoded bytes.Buffer
	if png.Encode(&encoded, processed) != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "image could not be normalized"})
		return
	}
	digest := sha256.Sum256(encoded.Bytes())
	publicID := uuid.NewString()
	objectKey := "ai-reference-sops/items/" + publicID + "/reference.png"
	thumbnailKey := "ai-reference-sops/items/" + publicID + "/thumbnail.png"
	maskKey := ""
	concrete, ok := s.storage.(interface {
		StoreReferenceDerivative(context.Context, string, string, []byte) error
	})
	if !ok || concrete.StoreReferenceDerivative(c.Request.Context(), objectKey, "image/png", encoded.Bytes()) != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "object_storage_unavailable", "message": "reference image could not be stored"})
		return
	}
	thumbnail, thumbnailErr := referenceThumbnail(processed)
	if thumbnailErr != nil || concrete.StoreReferenceDerivative(c.Request.Context(), thumbnailKey, "image/png", thumbnail) != nil {
		_ = s.storage.deleteSource(c.Request.Context(), objectKey)
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "object_storage_unavailable", "message": "reference thumbnail could not be stored"})
		return
	}
	if len(maskBytes) > 0 {
		maskKey = "ai-reference-sops/items/" + publicID + "/mask.png"
		if concrete.StoreReferenceDerivative(c.Request.Context(), maskKey, "image/png", maskBytes) != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"code": "object_storage_unavailable", "message": "reference mask could not be stored"})
			return
		}
	}
	var item models.AIReferenceItem
	err = s.db.Transaction(func(tx *gorm.DB) error {
		var version models.AIReferenceSOPVersion
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("public_id = ? AND status = ?", c.Param("version_id"), models.SOPVersionDraft).First(&version).Error; err != nil {
			return err
		}
		var max int
		if err := tx.Model(&models.AIReferenceItem{}).Where("ai_reference_sop_version_id = ?", version.ID).Select("COALESCE(MAX(sort_order),0)").Scan(&max).Error; err != nil {
			return err
		}
		item = models.AIReferenceItem{PublicID: publicID, AIReferenceSOPVersionID: version.ID, SortOrder: max + 1, Purpose: purpose, CaptionZH: captionZH, CaptionEN: captionEN, AllowedGuidanceZH: allowedZH, AllowedGuidanceEN: allowedEN, ForbiddenGuidanceZH: forbiddenZH, ForbiddenGuidanceEN: forbiddenEN, SourceName: sourceName, SourceURL: sourceURL, ObjectKey: objectKey, ThumbnailObjectKey: thumbnailKey, MaskObjectKey: maskKey, MIMEType: "image/png", Width: processed.Bounds().Dx(), Height: processed.Bounds().Dy(), ByteCount: int64(encoded.Len()), SHA256: hex.EncodeToString(digest[:]), RightsConfirmed: true, CreatedByID: currentUser(c).ID}
		return tx.Create(&item).Error
	})
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"code": "immutable_version", "message": "only draft versions can accept items"})
		return
	}
	c.JSON(http.StatusCreated, item)
}

func (s *Server) deleteAIReferenceItem(c *gin.Context) {
	if !requireUUIDParam(c, "version_id") || !requireUUIDParam(c, "item_id") {
		return
	}
	var item models.AIReferenceItem
	if err := s.db.Where("public_id = ? AND ai_reference_sop_version_id IN (SELECT id FROM ai_reference_sop_versions WHERE public_id = ? AND status = ?)", c.Param("item_id"), c.Param("version_id"), models.SOPVersionDraft).First(&item).Error; err != nil {
		c.JSON(http.StatusConflict, gin.H{"code": "immutable_version", "message": "only draft items can be deleted"})
		return
	}
	result := s.db.Delete(&item)
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "item could not be deleted"})
		return
	}
	if result.RowsAffected != 1 {
		c.JSON(http.StatusConflict, gin.H{"code": "immutable_version", "message": "only draft items can be deleted"})
		return
	}
	for _, objectKey := range []string{item.ObjectKey, item.ThumbnailObjectKey, item.MaskObjectKey} {
		if objectKey != "" {
			_ = s.storage.deleteSource(c.Request.Context(), objectKey)
		}
	}
	c.Status(http.StatusNoContent)
}

func (s *Server) reorderAIReferenceItems(c *gin.Context) {
	if !requireUUIDParam(c, "version_id") {
		return
	}
	var req struct {
		PublicIDs []string `json:"public_ids"`
	}
	if decodeJSONStrict(c, &req) != nil || len(req.PublicIDs) == 0 || !allUUIDs(req.PublicIDs) {
		respondAIBadRequest(c, errors.New("public_ids must be a non-empty UUID array"))
		return
	}
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var version models.AIReferenceSOPVersion
		if err := tx.Where("public_id=? AND status=?", c.Param("version_id"), models.SOPVersionDraft).First(&version).Error; err != nil {
			return err
		}
		var items []models.AIReferenceItem
		if err := tx.Where("ai_reference_sop_version_id=?", version.ID).Find(&items).Error; err != nil {
			return err
		}
		if len(items) != len(req.PublicIDs) {
			return errors.New("order mismatch")
		}
		known := map[string]bool{}
		for _, item := range items {
			known[item.PublicID] = true
		}
		for index, id := range req.PublicIDs {
			if !known[id] {
				return errors.New("order mismatch")
			}
			if err := tx.Model(&models.AIReferenceItem{}).Where("public_id=?", id).Update("sort_order", -(index + 1)).Error; err != nil {
				return err
			}
		}
		for index, id := range req.PublicIDs {
			if err := tx.Model(&models.AIReferenceItem{}).Where("public_id=?", id).Update("sort_order", index+1).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"code": "invalid_order", "message": "item order does not match the draft"})
		return
	}
	s.getAIReferenceVersion(c)
}

func (s *Server) publishAIReferenceSOPVersion(c *gin.Context) {
	s.transitionAIReferenceVersion(c, models.SOPVersionDraft, models.SOPVersionPublished)
}
func (s *Server) archiveAIReferenceSOPVersion(c *gin.Context) {
	s.transitionAIReferenceVersion(c, models.SOPVersionPublished, models.SOPVersionArchived)
}
func (s *Server) restoreAIReferenceSOPVersion(c *gin.Context) {
	s.transitionAIReferenceVersion(c, models.SOPVersionArchived, models.SOPVersionPublished)
}

func (s *Server) transitionAIReferenceVersion(c *gin.Context, from, to models.SOPVersionStatus) {
	if !requireUUIDParam(c, "version_id") {
		return
	}
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var version models.AIReferenceSOPVersion
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("public_id=? AND status=?", c.Param("version_id"), from).First(&version).Error; err != nil {
			return err
		}
		if to == models.SOPVersionPublished {
			if strings.TrimSpace(version.NameZH) == "" || strings.TrimSpace(version.NameEN) == "" || strings.TrimSpace(version.DescriptionZH) == "" || strings.TrimSpace(version.DescriptionEN) == "" {
				return errors.New("invalid")
			}
			var items []models.AIReferenceItem
			if err := tx.Where("ai_reference_sop_version_id=?", version.ID).Find(&items).Error; err != nil {
				return err
			}
			if len(items) == 0 {
				return errors.New("invalid")
			}
			for _, item := range items {
				if !validReferencePurpose(item.Purpose) || !item.RightsConfirmed || item.ObjectKey == "" || item.ThumbnailObjectKey == "" || item.SHA256 == "" || strings.TrimSpace(item.CaptionZH) == "" || strings.TrimSpace(item.CaptionEN) == "" || strings.TrimSpace(item.AllowedGuidanceZH) == "" || strings.TrimSpace(item.AllowedGuidanceEN) == "" || strings.TrimSpace(item.ForbiddenGuidanceZH) == "" || strings.TrimSpace(item.ForbiddenGuidanceEN) == "" || strings.TrimSpace(item.SourceName) == "" || (item.Purpose == models.AIReferenceVisualStyle && item.MaskObjectKey == "") {
					return errors.New("invalid")
				}
			}
		}
		now := time.Now()
		updates := map[string]any{"status": to}
		if to == models.SOPVersionPublished {
			updates["published_at"] = now
			updates["archived_at"] = nil
			updates["published_by_id"] = currentUser(c).ID
		} else {
			updates["archived_at"] = now
		}
		return tx.Model(&version).Updates(updates).Error
	})
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"code": "invalid_transition", "message": "version transition failed; publish requires bilingual metadata and at least one valid reference"})
		return
	}
	s.getAIReferenceVersion(c)
}

func (s *Server) aiReferenceItemMedia(c *gin.Context) {
	if !requireUUIDParam(c, "item_id") {
		return
	}
	var item models.AIReferenceItem
	if err := s.db.Where("public_id=?", c.Param("item_id")).First(&item).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": "not_found", "message": "reference item not found"})
		return
	}
	objectKey := item.ObjectKey
	if c.Query("thumbnail") == "true" && item.ThumbnailObjectKey != "" {
		objectKey = item.ThumbnailObjectKey
	}
	value, err := s.storage.ReadSource(c.Request.Context(), objectKey)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "object_storage_unavailable", "message": "reference image could not be read"})
		return
	}
	c.Data(http.StatusOK, value.MIMEType, value.Bytes)
}

func sortReferenceItems(values []models.AIReferenceItem) {
	sort.Slice(values, func(i, j int) bool { return values[i].SortOrder < values[j].SortOrder })
}
