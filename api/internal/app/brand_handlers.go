package app

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"cargoflows/api/internal/ai"
	"cargoflows/api/internal/models"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const maxActiveBrandIcons = 8

type brandDTO struct {
	PublicID     string `json:"public_id"`
	Name         string `json:"name"`
	IconCount    int64  `json:"icon_count"`
	ProductCount int64  `json:"product_count"`
}

type brandIconDTO struct {
	PublicID  string `json:"public_id"`
	Name      string `json:"name"`
	Notes     string `json:"notes"`
	MediaURL  string `json:"media_url"`
	MIMEType  string `json:"mime_type"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	ByteCount int64  `json:"byte_count"`
	SHA256    string `json:"sha256"`
	SortOrder int    `json:"sort_order"`
	Status    string `json:"status"`
}

func brandIconDTOFromModel(icon models.BrandIcon) brandIconDTO {
	return brandIconDTO{PublicID: icon.PublicID, Name: icon.Name, Notes: icon.Notes, MediaURL: "/api/v1/brand-icons/" + icon.PublicID + "/media", MIMEType: icon.MIMEType, Width: icon.Width, Height: icon.Height, ByteCount: icon.ByteCount, SHA256: icon.SHA256, SortOrder: icon.SortOrder, Status: icon.Status}
}

func (s *Server) listBrands(c *gin.Context) {
	var brands []models.Brand
	if err := s.db.Order("name ASC").Find(&brands).Error; err != nil {
		c.JSON(500, gin.H{"code": "internal_error", "message": "brands could not be loaded"})
		return
	}
	data := make([]brandDTO, 0, len(brands))
	for _, brand := range brands {
		var icons, products int64
		_ = s.db.Model(&models.BrandIcon{}).Where("brand_id = ? AND status = ?", brand.ID, "active").Count(&icons).Error
		_ = s.db.Model(&models.Product{}).Where("brand_id = ?", brand.ID).Count(&products).Error
		data = append(data, brandDTO{PublicID: brand.PublicID, Name: brand.Name, IconCount: icons, ProductCount: products})
	}
	c.JSON(http.StatusOK, gin.H{"data": data})
}

func (s *Server) createBrand(c *gin.Context) {
	var req struct {
		Name string `json:"name"`
	}
	if decodeJSONStrict(c, &req) != nil || strings.TrimSpace(req.Name) == "" || len([]rune(strings.TrimSpace(req.Name))) > 120 {
		c.JSON(400, gin.H{"code": "invalid_request", "message": "name is required and must be at most 120 characters"})
		return
	}
	name := strings.TrimSpace(req.Name)
	brand := models.Brand{PublicID: uuid.NewString(), Name: name, NameKey: strings.ToLower(name)}
	if err := s.db.Create(&brand).Error; err != nil {
		c.JSON(409, gin.H{"code": "brand_exists", "message": "brand already exists"})
		return
	}
	c.JSON(http.StatusCreated, brandDTO{PublicID: brand.PublicID, Name: brand.Name})
}

func (s *Server) updateBrand(c *gin.Context) {
	if !requireUUIDParam(c, "brand_id") {
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if decodeJSONStrict(c, &req) != nil || strings.TrimSpace(req.Name) == "" || len([]rune(strings.TrimSpace(req.Name))) > 120 {
		c.JSON(400, gin.H{"code": "invalid_request", "message": "name is required and must be at most 120 characters"})
		return
	}
	name := strings.TrimSpace(req.Name)
	var brand models.Brand
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("public_id = ?", c.Param("brand_id")).First(&brand).Error; err != nil {
			return err
		}
		if err := tx.Model(&brand).Updates(map[string]any{"name": name, "name_key": strings.ToLower(name)}).Error; err != nil {
			return err
		}
		return tx.Model(&models.Product{}).Where("brand_id = ?", brand.ID).Update("brand", name).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(404, gin.H{"code": "not_found", "message": "brand not found"})
		return
	}
	if err != nil {
		c.JSON(409, gin.H{"code": "brand_exists", "message": "brand already exists"})
		return
	}
	c.JSON(200, brandDTO{PublicID: brand.PublicID, Name: name})
}

func (s *Server) listBrandIcons(c *gin.Context) {
	if !requireUUIDParam(c, "brand_id") {
		return
	}
	var brand models.Brand
	if s.db.Where("public_id = ?", c.Param("brand_id")).First(&brand).Error != nil {
		c.JSON(404, gin.H{"code": "not_found", "message": "brand not found"})
		return
	}
	var icons []models.BrandIcon
	query := s.db.Where("brand_id = ?", brand.ID).Order("sort_order ASC, created_at ASC")
	if c.Query("status") == "active" {
		query = query.Where("status = ?", "active")
	}
	if err := query.Find(&icons).Error; err != nil {
		c.JSON(500, gin.H{"code": "internal_error", "message": "brand icons could not be loaded"})
		return
	}
	data := make([]brandIconDTO, 0, len(icons))
	for _, icon := range icons {
		data = append(data, brandIconDTOFromModel(icon))
	}
	c.JSON(200, gin.H{"data": data})
}

func (s *Server) createBrandIconUploadURL(c *gin.Context) {
	if !requireUUIDParam(c, "brand_id") {
		return
	}
	var req struct {
		FileName    string `json:"file_name"`
		ContentType string `json:"content_type"`
	}
	if decodeJSONStrict(c, &req) != nil {
		c.JSON(400, gin.H{"code": "invalid_request", "message": "file_name and content_type are required"})
		return
	}
	ext, ok := imageExtension(req.ContentType)
	if !ok {
		c.JSON(400, gin.H{"code": "unsupported_image", "message": "only PNG, JPEG, and WebP are supported"})
		return
	}
	var brand models.Brand
	if s.db.Where("public_id = ?", c.Param("brand_id")).First(&brand).Error != nil {
		c.JSON(404, gin.H{"code": "not_found", "message": "brand not found"})
		return
	}
	ticket := uuid.NewString()
	upload := models.BrandIconUpload{PublicID: ticket, BrandID: brand.ID, CreatedByID: currentUser(c).ID, TemporaryKey: "brand-icon-uploads/" + ticket + ext, ContentType: normalizedImageContentType(req.ContentType), ExpiresAt: time.Now().Add(15 * time.Minute)}
	if err := s.db.Create(&upload).Error; err != nil {
		c.JSON(500, gin.H{"code": "internal_error", "message": "upload could not be prepared"})
		return
	}
	url, _, err := s.storage.createUploadURL(c.Request.Context(), upload.TemporaryKey)
	if err != nil {
		_ = s.db.Delete(&upload).Error
		c.JSON(503, gin.H{"code": "object_storage_unavailable", "message": "upload could not be prepared"})
		return
	}
	c.JSON(200, gin.H{"method": "PUT", "upload_url": url, "completion_token": ticket, "expires_in": 900, "headers": gin.H{"content-type": upload.ContentType}})
}

func (s *Server) completeBrandIconUpload(c *gin.Context) {
	if !requireUUIDParam(c, "brand_id") {
		return
	}
	var req struct {
		CompletionToken string `json:"completion_token"`
		Name            string `json:"name"`
		Notes           string `json:"notes"`
	}
	if decodeJSONStrict(c, &req) != nil || !isUUID(req.CompletionToken) || strings.TrimSpace(req.Name) == "" || len([]rune(strings.TrimSpace(req.Name))) > 120 || len([]rune(req.Notes)) > 500 {
		c.JSON(400, gin.H{"code": "invalid_request", "message": "a valid completion_token and name are required"})
		return
	}
	var brand models.Brand
	if s.db.Where("public_id = ?", c.Param("brand_id")).First(&brand).Error != nil {
		c.JSON(404, gin.H{"code": "not_found", "message": "brand not found"})
		return
	}
	var upload models.BrandIconUpload
	if s.db.Where("public_id = ? AND brand_id = ? AND created_by_id = ? AND consumed_at IS NULL", req.CompletionToken, brand.ID, currentUser(c).ID).First(&upload).Error != nil || upload.ExpiresAt.Before(time.Now()) {
		c.JSON(400, gin.H{"code": "invalid_upload_ticket", "message": "upload ticket is invalid or expired"})
		return
	}
	source, err := s.storage.ReadSource(c.Request.Context(), upload.TemporaryKey)
	if err != nil {
		c.JSON(503, gin.H{"code": "object_storage_unavailable", "message": "uploaded icon could not be read"})
		return
	}
	validated, err := new(ai.ImageStorage).Validate(ai.ImageValidationRequest{Bytes: source.Bytes, MaxBytes: 5 << 20, MaxPixels: 20_000_000})
	if err != nil || validated.MIMEType != upload.ContentType {
		c.JSON(400, gin.H{"code": "invalid_image", "message": "uploaded icon is invalid or exceeds 5 MB"})
		return
	}
	var icon models.BrandIcon
	err = s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&models.Brand{}, brand.ID).Error; err != nil {
			return err
		}
		var active int64
		if err := tx.Model(&models.BrandIcon{}).Where("brand_id = ? AND status = ?", brand.ID, "active").Count(&active).Error; err != nil {
			return err
		}
		if active >= maxActiveBrandIcons {
			return errors.New("brand_icon_limit")
		}
		now := time.Now()
		claimed := tx.Model(&models.BrandIconUpload{}).Where("id = ? AND consumed_at IS NULL", upload.ID).Update("consumed_at", now)
		if claimed.Error != nil || claimed.RowsAffected != 1 {
			return errors.New("upload_claimed")
		}
		var maxOrder int
		_ = tx.Model(&models.BrandIcon{}).Where("brand_id = ?", brand.ID).Select("COALESCE(MAX(sort_order), 0)").Scan(&maxOrder).Error
		icon = models.BrandIcon{PublicID: uuid.NewString(), BrandID: brand.ID, Name: strings.TrimSpace(req.Name), Notes: strings.TrimSpace(req.Notes), MIMEType: validated.MIMEType, Width: validated.Width, Height: validated.Height, ByteCount: int64(len(source.Bytes)), SHA256: validated.SHA256, SortOrder: maxOrder + 1, Status: "active"}
		ext, _ := imageExtension(validated.MIMEType)
		icon.ObjectKey = "brand-icons/final/" + icon.PublicID + ext
		return tx.Create(&icon).Error
	})
	if err != nil {
		code := 500
		message := "brand icon could not be created"
		if err.Error() == "brand_icon_limit" {
			code = 409
			message = "a brand may have at most 8 active icons"
		}
		c.JSON(code, gin.H{"code": "brand_icon_limit", "message": message})
		return
	}
	if err := s.storage.promoteSource(c.Request.Context(), upload.TemporaryKey, icon.ObjectKey, icon.MIMEType, source.Bytes); err != nil {
		_ = s.db.Delete(&icon).Error
		_ = s.db.Model(&models.BrandIconUpload{}).Where("id = ?", upload.ID).Update("consumed_at", nil).Error
		c.JSON(503, gin.H{"code": "object_storage_unavailable", "message": "brand icon could not be finalized"})
		return
	}
	c.JSON(201, brandIconDTOFromModel(icon))
}

func (s *Server) updateBrandIcon(c *gin.Context) {
	if !requireUUIDParam(c, "brand_id") || !requireUUIDParam(c, "icon_id") {
		return
	}
	var req struct {
		Name   *string `json:"name"`
		Notes  *string `json:"notes"`
		Status *string `json:"status"`
	}
	if decodeJSONStrict(c, &req) != nil {
		c.JSON(400, gin.H{"code": "invalid_request", "message": "invalid brand icon update"})
		return
	}
	var brand models.Brand
	if s.db.Where("public_id = ?", c.Param("brand_id")).First(&brand).Error != nil {
		c.JSON(404, gin.H{"code": "not_found", "message": "brand not found"})
		return
	}
	var icon models.BrandIcon
	if s.db.Where("public_id = ? AND brand_id = ?", c.Param("icon_id"), brand.ID).First(&icon).Error != nil {
		c.JSON(404, gin.H{"code": "not_found", "message": "brand icon not found"})
		return
	}
	updates := map[string]any{}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" || len([]rune(name)) > 120 {
			c.JSON(400, gin.H{"code": "invalid_request", "message": "name is required"})
			return
		}
		updates["name"] = name
	}
	if req.Notes != nil {
		if len([]rune(*req.Notes)) > 500 {
			c.JSON(400, gin.H{"code": "invalid_request", "message": "notes are too long"})
			return
		}
		updates["notes"] = strings.TrimSpace(*req.Notes)
	}
	if req.Status != nil {
		if *req.Status != "active" && *req.Status != "disabled" {
			c.JSON(400, gin.H{"code": "invalid_request", "message": "status must be active or disabled"})
			return
		}
		updates["status"] = *req.Status
	}
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if req.Status != nil && *req.Status == "active" && icon.Status != "active" {
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&models.Brand{}, brand.ID).Error; err != nil {
				return err
			}
			var count int64
			if err := tx.Model(&models.BrandIcon{}).Where("brand_id = ? AND status = ?", brand.ID, "active").Count(&count).Error; err != nil {
				return err
			}
			if count >= maxActiveBrandIcons {
				return errors.New("brand_icon_limit")
			}
		}
		return tx.Model(&icon).Updates(updates).Error
	})
	if err != nil {
		if err.Error() == "brand_icon_limit" {
			c.JSON(409, gin.H{"code": "brand_icon_limit", "message": "a brand may have at most 8 active icons"})
			return
		}
		c.JSON(500, gin.H{"code": "internal_error", "message": "brand icon could not be updated"})
		return
	}
	_ = s.db.First(&icon, icon.ID).Error
	c.JSON(200, brandIconDTOFromModel(icon))
}

func (s *Server) reorderBrandIcons(c *gin.Context) {
	if !requireUUIDParam(c, "brand_id") {
		return
	}
	var req struct {
		IconIDs []string `json:"icon_ids"`
	}
	if decodeJSONStrict(c, &req) != nil {
		c.JSON(400, gin.H{"code": "invalid_request", "message": "icon_ids are required"})
		return
	}
	var brand models.Brand
	if s.db.Where("public_id = ?", c.Param("brand_id")).First(&brand).Error != nil {
		c.JSON(404, gin.H{"code": "not_found", "message": "brand not found"})
		return
	}
	var icons []models.BrandIcon
	_ = s.db.Where("brand_id = ?", brand.ID).Find(&icons).Error
	if len(icons) != len(req.IconIDs) {
		c.JSON(400, gin.H{"code": "invalid_request", "message": "icon_ids must contain every brand icon exactly once"})
		return
	}
	seen := map[string]bool{}
	for _, id := range req.IconIDs {
		if seen[id] || !isUUID(id) {
			c.JSON(400, gin.H{"code": "invalid_request", "message": "icon_ids must be unique UUIDs"})
			return
		}
		seen[id] = true
	}
	for _, icon := range icons {
		if !seen[icon.PublicID] {
			c.JSON(400, gin.H{"code": "invalid_request", "message": "icon_ids contain an icon from another brand"})
			return
		}
	}
	err := s.db.Transaction(func(tx *gorm.DB) error {
		for i, id := range req.IconIDs {
			if err := tx.Model(&models.BrandIcon{}).Where("public_id = ? AND brand_id = ?", id, brand.ID).Update("sort_order", i+1).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		c.JSON(500, gin.H{"code": "internal_error", "message": "brand icons could not be reordered"})
		return
	}
	s.listBrandIcons(c)
}

func (s *Server) brandIconMedia(c *gin.Context) {
	if !requireUUIDParam(c, "icon_id") {
		return
	}
	var icon models.BrandIcon
	if s.db.Where("public_id = ?", c.Param("icon_id")).First(&icon).Error != nil {
		c.JSON(404, gin.H{"code": "not_found", "message": "brand icon not found"})
		return
	}
	source, err := s.storage.ReadSource(c.Request.Context(), icon.ObjectKey)
	if err != nil {
		c.JSON(503, gin.H{"code": "object_storage_unavailable", "message": "brand icon could not be read"})
		return
	}
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Disposition", "inline")
	c.Data(200, icon.MIMEType, source.Bytes)
}
