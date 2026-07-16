package app

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"cargoflow/api/internal/models"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type loginRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

func (s *Server) login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}

	var user models.User
	if err := s.db.Where("email = ? AND status = ?", req.Email, "active").First(&user).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"message": "invalid credentials"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"message": "invalid credentials"})
		return
	}

	user.LastSeenAt = time.Now()
	_ = s.db.Save(&user).Error

	token, err := s.issueToken(user)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "issue token failed"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"token": token, "user": user})
}

type createSKURequest struct {
	CategoryID        uint     `json:"category_id"`
	ProductName       string   `json:"product_name" binding:"required"`
	Brand             string   `json:"brand"`
	Category          string   `json:"category"`
	Code              string   `json:"code" binding:"required"`
	Color             string   `json:"color"`
	Size              string   `json:"size"`
	Barcode           string   `json:"barcode"`
	Stock             int      `json:"stock"`
	LowStockThreshold int      `json:"low_stock_threshold"`
	PlatformTitle     string   `json:"platform_title"`
	SellingPoints     string   `json:"selling_points"`
	Status            string   `json:"status"`
	Tags              []string `json:"tags"`
}

func (s *Server) listSKUs(c *gin.Context) {
	var skus []models.SKU
	query := s.db.Preload("Product.CatalogCategory").Preload("Tags").Joins("JOIN products ON products.id = skus.product_id").Order("skus.updated_at DESC")
	if categoryID := c.Query("category_id"); categoryID != "" {
		query = query.Where("products.category_id = ?", categoryID)
	}
	if search := c.Query("q"); search != "" {
		like := "%" + search + "%"
		query = query.Where("skus.code LIKE ? OR products.name LIKE ? OR products.category LIKE ? OR EXISTS (SELECT 1 FROM sku_tags JOIN tags ON tags.id = sku_tags.tag_id WHERE sku_tags.sku_id = skus.id AND tags.name LIKE ?)", like, like, like, like)
	}
	if err := query.Find(&skus).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": skus})
}

func (s *Server) createSKU(c *gin.Context) {
	var req createSKURequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}

	if req.Status == "" {
		req.Status = "active"
	}

	var sku models.SKU
	err := s.db.Transaction(func(tx *gorm.DB) error {
		category, err := s.resolveCategory(tx, req.CategoryID, req.Category)
		if err != nil {
			return err
		}
		product := models.Product{CategoryID: category.ID, Name: req.ProductName, Brand: req.Brand, Category: category.Name}
		if err := tx.Create(&product).Error; err != nil {
			return err
		}
		sku = models.SKU{
			ProductID:         product.ID,
			Code:              req.Code,
			Color:             req.Color,
			Size:              req.Size,
			Barcode:           req.Barcode,
			Stock:             req.Stock,
			LowStockThreshold: req.LowStockThreshold,
			PlatformTitle:     req.PlatformTitle,
			SellingPoints:     req.SellingPoints,
			Status:            req.Status,
		}
		if err := tx.Create(&sku).Error; err != nil {
			return err
		}
		tags, err := resolveTags(tx, req.Tags)
		if err != nil {
			return err
		}
		if err := tx.Model(&sku).Association("Tags").Replace(tags); err != nil {
			return err
		}
		return tx.Preload("Product.CatalogCategory").Preload("Tags").First(&sku, sku.ID).Error
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, sku)
}

func (s *Server) getSKU(c *gin.Context) {
	var sku models.SKU
	if err := s.db.Preload("Product.CatalogCategory").Preload("Tags").First(&sku, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "sku not found"})
		return
	}
	c.JSON(http.StatusOK, sku)
}

func (s *Server) updateSKU(c *gin.Context) {
	var sku models.SKU
	if err := s.db.Preload("Product.CatalogCategory").Preload("Tags").First(&sku, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "sku not found"})
		return
	}

	var req createSKURequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}

	err := s.db.Transaction(func(tx *gorm.DB) error {
		category, err := s.resolveCategory(tx, req.CategoryID, req.Category)
		if err != nil {
			return err
		}
		sku.Code = req.Code
		sku.Color = req.Color
		sku.Size = req.Size
		sku.Barcode = req.Barcode
		sku.LowStockThreshold = req.LowStockThreshold
		sku.PlatformTitle = req.PlatformTitle
		sku.SellingPoints = req.SellingPoints
		if req.Status != "" {
			sku.Status = req.Status
		}
		if err := tx.Save(&sku).Error; err != nil {
			return err
		}

		sku.Product.Name = req.ProductName
		sku.Product.Brand = req.Brand
		sku.Product.CategoryID = category.ID
		sku.Product.Category = category.Name
		if err := tx.Save(&sku.Product).Error; err != nil {
			return err
		}
		if req.Tags != nil {
			tags, err := resolveTags(tx, req.Tags)
			if err != nil {
				return err
			}
			return tx.Model(&sku).Association("Tags").Replace(tags)
		}
		return nil
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}

	_ = s.db.Preload("Product.CatalogCategory").Preload("Tags").First(&sku, sku.ID).Error
	c.JSON(http.StatusOK, sku)
}

type inventoryAdjustmentRequest struct {
	QuantityDelta int    `json:"quantity_delta" binding:"required"`
	Reason        string `json:"reason" binding:"required"`
	Note          string `json:"note"`
}

func (s *Server) createInventoryAdjustment(c *gin.Context) {
	skuID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid sku id"})
		return
	}

	var req inventoryAdjustmentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}

	user := currentUser(c)
	var adjustment models.InventoryAdjustment
	err = s.db.Transaction(func(tx *gorm.DB) error {
		var sku models.SKU
		if err := tx.First(&sku, skuID).Error; err != nil {
			return err
		}
		sku.Stock += req.QuantityDelta
		if sku.Stock < 0 {
			return fmt.Errorf("stock cannot be negative")
		}
		if err := tx.Save(&sku).Error; err != nil {
			return err
		}
		adjustment = models.InventoryAdjustment{
			SKUID:         uint(skuID),
			QuantityDelta: req.QuantityDelta,
			Reason:        req.Reason,
			Note:          req.Note,
			OperatorID:    user.ID,
		}
		return tx.Create(&adjustment).Error
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, adjustment)
}

func (s *Server) listInventoryHistory(c *gin.Context) {
	var adjustments []models.InventoryAdjustment
	if err := s.db.Preload("Operator").Where("sku_id = ?", c.Param("id")).Order("created_at DESC").Find(&adjustments).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": adjustments})
}

type createPhotoSessionRequest struct {
	SKUID        uint   `json:"sku_id"`
	SOPVersionID string `json:"sop_version_id"`
}

type photoSessionResponse struct {
	PublicID     string    `json:"public_id"`
	Code         string    `json:"code"`
	SKUID        uint      `json:"sku_id"`
	SOPVersionID string    `json:"sop_version_id"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"created_at"`
}

var (
	errCaptureVersionNotFound = errors.New("SOP version not found")
	errVersionNotPublished    = errors.New("SOP version is not published")
	errSKUNotFound            = errors.New("SKU not found")
	errSKUCategoryMismatch    = errors.New("SKU category does not match the capture SOP category")
	errPhotoSessionNotFound   = errors.New("photo session not found")
	errPhotoSessionForbidden  = errors.New("photo session does not belong to the current user")
	errSOPViewNotFound        = errors.New("SOP view not found")
	errViewVersionMismatch    = errors.New("SOP view does not belong to the session version")
	errInvalidUploadTicket    = errors.New("upload completion ticket is invalid")
	errUploadedObjectNotFound = errors.New("uploaded object was not found")
)

func (s *Server) createPhotoSession(c *gin.Context) {
	var req createPhotoSessionRequest
	if err := decodeJSONStrict(c, &req); err != nil || req.SKUID == 0 || !isUUID(req.SOPVersionID) {
		respondSOPBadRequest(c, errOr(err, "sku_id and a UUID sop_version_id are required"))
		return
	}
	user := currentUser(c)
	var session models.PhotoSession
	var selectedVersionPublicID string
	err := s.db.WithContext(c).Transaction(func(tx *gorm.DB) error {
		var version models.SOPVersion
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("public_id = ?", req.SOPVersionID).First(&version).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errCaptureVersionNotFound
			}
			return err
		}
		if version.Status != models.SOPVersionPublished {
			return errVersionNotPublished
		}
		var captureSOP models.CaptureSOP
		if err := tx.Select("id", "category_id").First(&captureSOP, version.CaptureSOPID).Error; err != nil {
			return err
		}
		var sku models.SKU
		if err := tx.Model(&models.SKU{}).Select("skus.*").
			Joins("JOIN products ON products.id = skus.product_id").
			Preload("Product").Where("skus.id = ?", req.SKUID).First(&sku).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errSKUNotFound
			}
			return err
		}
		if sku.Product.CategoryID != captureSOP.CategoryID {
			return errSKUCategoryMismatch
		}
		selectedVersionPublicID = version.PublicID
		session = models.PhotoSession{
			PublicID: uuid.NewString(), Code: fmt.Sprintf("PS-%s-%d", time.Now().Format("20060102"), time.Now().UnixNano()),
			SKUID: req.SKUID, SOPVersionID: version.ID, PhotographerID: user.ID, Status: "in_progress",
		}
		return tx.Create(&session).Error
	})
	if err != nil {
		respondCaptureError(c, err)
		return
	}
	c.JSON(http.StatusCreated, photoSessionResponse{PublicID: session.PublicID, Code: session.Code, SKUID: session.SKUID, SOPVersionID: selectedVersionPublicID, Status: session.Status, CreatedAt: session.CreatedAt})
}

type uploadURLRequest struct {
	FileName       string `json:"file_name"`
	ContentType    string `json:"content_type"`
	PhotoSessionID string `json:"photo_session_id"`
	SOPViewID      string `json:"sop_view_id"`
}

type assetUploadClaims struct {
	PhotoSessionID string `json:"photo_session_id"`
	SOPViewID      string `json:"sop_view_id"`
	ObjectKey      string `json:"object_key"`
	UserID         uint   `json:"user_id"`
	jwt.RegisteredClaims
}

func (s *Server) createUploadURL(c *gin.Context) {
	var req uploadURLRequest
	if err := decodeJSONStrict(c, &req); err != nil || strings.TrimSpace(req.FileName) == "" || !isUUID(req.PhotoSessionID) || !isUUID(req.SOPViewID) {
		respondSOPBadRequest(c, errOr(err, "file_name, photo_session_id, and sop_view_id are required; identifiers must be UUIDs"))
		return
	}
	if !strings.HasPrefix(req.ContentType, "image/") {
		respondSOPBadRequest(c, errors.New("only image uploads are supported"))
		return
	}
	if _, _, err := s.resolveCaptureBinding(c, req.PhotoSessionID, req.SOPViewID); err != nil {
		respondCaptureError(c, err)
		return
	}

	extension, ok := imageExtension(req.ContentType)
	if !ok {
		respondSOPBadRequest(c, errors.New("unsupported image content type"))
		return
	}
	objectKey := fmt.Sprintf("photo-sessions/%s/views/%s/%s%s", req.PhotoSessionID, req.SOPViewID, uuid.NewString(), extension)
	uploadURL, assetURL, err := s.storage.createUploadURL(c.Request.Context(), objectKey)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "object_storage_unavailable", "message": "prepare object storage upload failed"})
		return
	}
	completionToken, err := s.issueAssetUploadTicket(currentUser(c).ID, req.PhotoSessionID, req.SOPViewID, objectKey)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "issue upload completion ticket failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"method":           "PUT",
		"upload_url":       uploadURL,
		"asset_url":        assetURL,
		"object_key":       objectKey,
		"completion_token": completionToken,
		"expires_in":       900,
		"headers":          gin.H{"content-type": req.ContentType},
	})
}

type completeAssetRequest struct {
	PhotoSessionID  string `json:"photo_session_id"`
	SOPViewID       string `json:"sop_view_id"`
	CompletionToken string `json:"completion_token"`
	CapturedAt      string `json:"captured_at"`
}

type completedAssetResponse struct {
	ID             uint      `json:"id"`
	SKUID          uint      `json:"sku_id"`
	PhotoSessionID string    `json:"photo_session_id"`
	SOPViewID      string    `json:"sop_view_id"`
	ObjectKey      string    `json:"object_key"`
	OriginalURL    string    `json:"original_url"`
	ThumbnailURL   string    `json:"thumbnail_url"`
	ReviewStatus   string    `json:"review_status"`
	CapturedAt     time.Time `json:"captured_at"`
}

func (s *Server) completeAssetUpload(c *gin.Context) {
	var req completeAssetRequest
	if err := decodeJSONStrict(c, &req); err != nil || !isUUID(req.PhotoSessionID) || !isUUID(req.SOPViewID) || strings.TrimSpace(req.CompletionToken) == "" {
		respondSOPBadRequest(c, errOr(err, "photo_session_id, sop_view_id, and completion_token are required; identifiers must be UUIDs"))
		return
	}
	capturedAt := time.Now()
	if req.CapturedAt != "" {
		parsed, err := time.Parse(time.RFC3339, req.CapturedAt)
		if err != nil {
			respondSOPBadRequest(c, errors.New("captured_at must be an RFC3339 timestamp"))
			return
		}
		capturedAt = parsed
	}
	session, view, err := s.resolveCaptureBinding(c, req.PhotoSessionID, req.SOPViewID)
	if err != nil {
		respondCaptureError(c, err)
		return
	}
	claims, err := s.verifyAssetUploadTicket(req.CompletionToken)
	if err != nil || claims.UserID != currentUser(c).ID || claims.PhotoSessionID != session.PublicID || claims.SOPViewID != view.PublicID || !isScopedAssetObjectKey(claims.ObjectKey, session.PublicID, view.PublicID) {
		respondCaptureError(c, errInvalidUploadTicket)
		return
	}
	exists, err := s.storage.objectExists(c.Request.Context(), claims.ObjectKey)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "object_storage_unavailable", "message": "verify uploaded object failed"})
		return
	}
	if !exists {
		respondCaptureError(c, errUploadedObjectNotFound)
		return
	}
	asset := models.Asset{
		SKUID:          session.SKUID,
		PhotoSessionID: session.ID,
		SOPViewID:      view.ID,
		ObjectKey:      claims.ObjectKey,
		OriginalURL:    s.storage.assetURL(claims.ObjectKey),
		ReviewStatus:   "pending",
		CapturedAt:     capturedAt,
	}
	if err := s.db.Create(&asset).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "internal server error"})
		return
	}
	c.JSON(http.StatusCreated, completedAssetResponse{ID: asset.ID, SKUID: asset.SKUID, PhotoSessionID: session.PublicID, SOPViewID: view.PublicID, ObjectKey: asset.ObjectKey, OriginalURL: asset.OriginalURL, ThumbnailURL: asset.ThumbnailURL, ReviewStatus: asset.ReviewStatus, CapturedAt: asset.CapturedAt})
}

func imageExtension(contentType string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0])) {
	case "image/jpeg":
		return ".jpg", true
	case "image/png":
		return ".png", true
	case "image/heic", "image/heif":
		return ".heic", true
	case "image/webp":
		return ".webp", true
	default:
		return "", false
	}
}

func (s *Server) issueAssetUploadTicket(userID uint, sessionID, viewID, objectKey string) (string, error) {
	now := time.Now()
	claims := assetUploadClaims{
		PhotoSessionID: sessionID,
		SOPViewID:      viewID,
		ObjectKey:      objectKey,
		UserID:         userID,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(15 * time.Minute)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(s.cfg.JWTSecret))
}

func (s *Server) verifyAssetUploadTicket(value string) (*assetUploadClaims, error) {
	claims := &assetUploadClaims{}
	token, err := jwt.ParseWithClaims(value, claims, func(token *jwt.Token) (any, error) {
		return []byte(s.cfg.JWTSecret), nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithExpirationRequired())
	if err != nil || !token.Valid {
		return nil, errInvalidUploadTicket
	}
	return claims, nil
}

func isScopedAssetObjectKey(objectKey, sessionID, viewID string) bool {
	prefix := "photo-sessions/" + sessionID + "/views/" + viewID + "/"
	base := strings.TrimPrefix(objectKey, prefix)
	if base == objectKey || base == "" || strings.Contains(base, "/") || strings.Contains(base, "\\") {
		return false
	}
	for _, extension := range []string{".jpg", ".png", ".heic", ".webp"} {
		if strings.HasSuffix(base, extension) {
			_, err := uuid.Parse(strings.TrimSuffix(base, extension))
			return err == nil
		}
	}
	return false
}

func (s *Server) resolveCaptureBinding(c *gin.Context, sessionPublicID, viewPublicID string) (models.PhotoSession, models.SOPView, error) {
	var session models.PhotoSession
	if err := s.db.WithContext(c).Where("public_id = ?", sessionPublicID).First(&session).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return session, models.SOPView{}, errPhotoSessionNotFound
		}
		return session, models.SOPView{}, err
	}
	if session.PhotographerID != currentUser(c).ID {
		return session, models.SOPView{}, errPhotoSessionForbidden
	}
	var view models.SOPView
	if err := s.db.WithContext(c).Where("public_id = ?", viewPublicID).First(&view).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return session, view, errSOPViewNotFound
		}
		return session, view, err
	}
	if view.SOPVersionID != session.SOPVersionID {
		return session, view, errViewVersionMismatch
	}
	return session, view, nil
}

func respondCaptureError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, errVersionNotPublished):
		c.JSON(http.StatusConflict, gin.H{"code": "version_not_published", "message": err.Error()})
	case errors.Is(err, errViewVersionMismatch):
		c.JSON(http.StatusConflict, gin.H{"code": "view_version_mismatch", "message": err.Error()})
	case errors.Is(err, errCaptureVersionNotFound):
		c.JSON(http.StatusNotFound, gin.H{"code": "version_not_found", "message": err.Error()})
	case errors.Is(err, errSKUNotFound):
		c.JSON(http.StatusNotFound, gin.H{"code": "sku_not_found", "message": err.Error()})
	case errors.Is(err, errSKUCategoryMismatch):
		c.JSON(http.StatusConflict, gin.H{"code": "sku_sop_category_mismatch", "message": err.Error()})
	case errors.Is(err, errPhotoSessionNotFound):
		c.JSON(http.StatusNotFound, gin.H{"code": "photo_session_not_found", "message": err.Error()})
	case errors.Is(err, errPhotoSessionForbidden):
		c.JSON(http.StatusForbidden, gin.H{"code": "photo_session_forbidden", "message": err.Error()})
	case errors.Is(err, errSOPViewNotFound):
		c.JSON(http.StatusNotFound, gin.H{"code": "sop_view_not_found", "message": err.Error()})
	case errors.Is(err, errInvalidUploadTicket):
		c.JSON(http.StatusBadRequest, gin.H{"code": "invalid_upload_ticket", "message": err.Error()})
	case errors.Is(err, errUploadedObjectNotFound):
		c.JSON(http.StatusConflict, gin.H{"code": "upload_not_found", "message": err.Error()})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "internal server error"})
	}
}

func (s *Server) listAssetsForReview(c *gin.Context) {
	var assets []models.Asset
	query := s.db.Preload("SKU.Product.CatalogCategory").Preload("SKU.Tags").Preload("SOPView").Preload("PhotoSession").Order("created_at DESC")
	if status := c.Query("status"); status != "" {
		query = query.Where("review_status = ?", status)
	}
	if err := query.Find(&assets).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	items := make([]assetReviewItem, 0, len(assets))
	for _, asset := range assets {
		items = append(items, assetReviewItem{
			ID: asset.ID, SKUID: asset.SKUID, OriginalURL: asset.OriginalURL, ThumbnailURL: asset.ThumbnailURL,
			ReviewStatus: asset.ReviewStatus, CapturedAt: asset.CapturedAt,
			SOPViewName:      localizedViewName{ZHCN: asset.SOPView.NameZH, EN: asset.SOPView.NameEN},
			PhotoSessionCode: asset.PhotoSession.Code,
		})
	}
	c.JSON(http.StatusOK, gin.H{"data": items})
}

type assetReviewItem struct {
	ID               uint              `json:"id"`
	SKUID            uint              `json:"sku_id"`
	OriginalURL      string            `json:"original_url"`
	ThumbnailURL     string            `json:"thumbnail_url"`
	ReviewStatus     string            `json:"review_status"`
	CapturedAt       time.Time         `json:"captured_at"`
	SOPViewName      localizedViewName `json:"sop_view_name"`
	PhotoSessionCode string            `json:"photo_session_code"`
}

type reviewAssetRequest struct {
	Status string `json:"status" binding:"required"`
	Reason string `json:"reason"`
}

func (s *Server) reviewAsset(c *gin.Context) {
	var req reviewAssetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	user := currentUser(c)
	var asset models.Asset
	if err := s.db.First(&asset, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "asset not found"})
		return
	}
	asset.ReviewStatus = req.Status
	if err := s.db.Save(&asset).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	_ = s.db.Create(&models.AssetReview{AssetID: asset.ID, ReviewerID: user.ID, Status: req.Status, Reason: req.Reason}).Error
	c.JSON(http.StatusOK, asset)
}

func (s *Server) listAIJobs(c *gin.Context) {
	var jobs []models.AIJob
	if err := s.db.Preload("SKU.Product").Order("created_at DESC").Find(&jobs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": jobs})
}

type aiJobRequest struct {
	SKUID          uint   `json:"sku_id" binding:"required"`
	TargetPlatform string `json:"target_platform" binding:"required"`
	InputAssetIDs  string `json:"input_asset_ids"`
}

func (s *Server) createAIJob(c *gin.Context) {
	var req aiJobRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	user := currentUser(c)
	job := models.AIJob{
		SKUID:          req.SKUID,
		TargetPlatform: req.TargetPlatform,
		Status:         "pending",
		InputAssetIDs:  req.InputAssetIDs,
		CreatedByID:    user.ID,
	}
	if err := s.db.Create(&job).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, job)
}

func (s *Server) listUsers(c *gin.Context) {
	var users []models.User
	if err := s.db.Order("created_at DESC").Find(&users).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": users})
}
