package app

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"time"

	"cargoflows/api/internal/ai"
	"cargoflows/api/internal/config"
	"cargoflows/api/internal/models"
	"cargoflows/api/internal/secrets"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"gorm.io/gorm"
)

type Server struct {
	cfg     config.Config
	db      *gorm.DB
	storage assetStorage
	ai      AIDependencies
}

type AIDependencies struct {
	ProviderSettings *ai.ProviderSettingsService
	Templates        *ai.TemplateService
	Jobs             *ai.JobService
	TextResults      *ai.TextResultService
	ImageResults     *ai.ImageResultService
}

func NewRouter(cfg config.Config, db *gorm.DB) *gin.Engine {
	deps, err := newAIDependencies(cfg, db)
	if err != nil {
		panic("configure AI services: " + err.Error())
	}
	return newRouter(cfg, db, deps)
}

func NewRouterWithAIDependencies(cfg config.Config, db *gorm.DB, deps AIDependencies) *gin.Engine {
	if _, err := validateSecretsMasterKey(cfg); err != nil {
		panic("configure AI services: " + err.Error())
	}
	return newRouter(cfg, db, deps)
}

func newRouter(cfg config.Config, db *gorm.DB, deps AIDependencies) *gin.Engine {
	if cfg.AppEnv == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	storage, err := newObjectStore(cfg)
	if err != nil {
		panic("configure object storage: " + err.Error())
	}
	if deps.Templates == nil {
		deps.Templates = ai.NewTemplateService(db)
	}
	if deps.Jobs == nil {
		deps.Jobs = ai.NewJobService(db)
	}
	if deps.TextResults == nil {
		deps.TextResults = ai.NewTextResultService(db)
	}
	if deps.ImageResults == nil {
		deps.ImageResults = ai.NewImageResultService(db)
	}
	server := &Server{cfg: cfg, db: db, storage: storage, ai: deps}
	router := gin.New()
	router.Use(gin.Logger(), gin.Recovery())

	router.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	v1 := router.Group("/api/v1")
	v1.POST("/auth/login", server.login)

	protected := v1.Group("")
	protected.Use(server.requireAuth())
	protected.GET("/auth/me", server.me)
	protected.POST("/auth/change-password", server.changePassword)
	protected.Use(server.requirePasswordChanged())
	registerExistingRoutes(protected, server)
	registerAIRoutes(protected, server)

	return router
}

func registerExistingRoutes(protected *gin.RouterGroup, server *Server) {
	protected.GET("/skus", server.listSKUs)
	protected.POST("/skus", server.createSKU)
	protected.GET("/skus/:sku_id", server.getSKU)
	protected.PATCH("/skus/:sku_id", server.updateSKU)
	protected.DELETE("/skus/:sku_id", server.deleteSKU)
	protected.POST("/skus/:sku_id/inventory-adjustments", server.createInventoryAdjustment)
	protected.GET("/skus/:sku_id/inventory-history", server.listInventoryHistory)
	protected.GET("/skus/:sku_id/variant-identity", server.getSKUVariantIdentity)
	protected.GET("/categories", server.listCategories)
	protected.POST("/categories", server.createCategory)
	protected.DELETE("/categories/:id", server.deleteCategory)
	protected.GET("/tags", server.listTags)
	protected.GET("/brands", server.listBrands)
	protected.GET("/brands/:brand_id/icons", server.listBrandIcons)
	protected.GET("/brand-icons/:icon_id/media", server.brandIconMedia)
	brandManagers := protected.Group("")
	brandManagers.Use(requireRoles(models.RoleSuperAdmin, models.RoleAdmin))
	brandManagers.POST("/brands", server.createBrand)
	brandManagers.PATCH("/brands/:brand_id", server.updateBrand)
	brandManagers.POST("/brands/:brand_id/icons/upload-url", server.createBrandIconUploadURL)
	brandManagers.POST("/brands/:brand_id/icons", server.completeBrandIconUpload)
	brandManagers.PATCH("/brands/:brand_id/icons/:icon_id", server.updateBrandIcon)
	brandManagers.PUT("/brands/:brand_id/icon-order", server.reorderBrandIcons)
	protected.GET("/capture-sops", server.listCaptureSOPs)
	protected.GET("/capture-sops/:sop_id", server.getCaptureSOP)
	protected.GET("/sop-versions/:version_id", server.getSOPVersion)
	protected.GET("/sop-reference-images/:image_id/media", server.sopReferenceMedia)
	protected.GET("/model-families", server.listModelFamilies)
	protected.GET("/model-families/:family_id", server.getModelFamily)
	modelFamilyAdmins := protected.Group("")
	modelFamilyAdmins.Use(requireRoles(models.RoleSuperAdmin, models.RoleAdmin))
	modelFamilyAdmins.POST("/model-families", server.createModelFamily)
	modelFamilyAdmins.PATCH("/model-families/:family_id", server.updateModelFamily)
	modelFamilyManagers := protected.Group("")
	modelFamilyManagers.Use(requireRoles(models.RoleSuperAdmin, models.RoleAdmin, models.RoleOperator))
	modelFamilyManagers.POST("/model-families/:family_id/members", server.addModelFamilyMember)
	modelFamilyManagers.POST("/model-families/:family_id/reference-assets", server.createModelFamilyReferenceAsset)
	modelFamilyManagers.GET("/model-families/:family_id/reference-assets", server.listModelFamilyReferenceAssets)
	modelFamilyManagers.PATCH("/model-families/:family_id/reference-assets/:reference_id", server.revokeModelFamilyReferenceAsset)
	modelFamilyManagers.DELETE("/model-families/:family_id/members/:member_id", server.removeModelFamilyMember)
	modelFamilyManagers.POST("/skus/:sku_id/variant-identity/versions", server.createSKUVariantIdentityVersion)
	modelFamilyManagers.PATCH("/variant-identity-versions/:version_id", server.updateVariantIdentityVersion)
	modelFamilyManagers.POST("/variant-identity-versions/:version_id/validate", server.validateVariantIdentityVersion)
	modelFamilyManagers.POST("/variant-identity-versions/:version_id/publish", server.publishVariantIdentityVersion)
	sopManagers := protected.Group("")
	sopManagers.Use(requireRoles(models.RoleSuperAdmin, models.RoleAdmin, models.RoleOperator))
	sopManagers.POST("/capture-sops", server.createCaptureSOP)
	sopManagers.PATCH("/sop-versions/:version_id", server.updateSOPVersion)
	sopManagers.POST("/sop-versions/:version_id/views", server.addSOPView)
	sopManagers.PATCH("/sop-versions/:version_id/views/:view_id", server.updateSOPView)
	sopManagers.DELETE("/sop-versions/:version_id/views/:view_id", server.deleteSOPView)
	sopManagers.PUT("/sop-versions/:version_id/view-order", server.reorderSOPViews)
	sopManagers.POST("/sop-versions/:version_id/validate", server.validateSOPVersion)
	sopManagers.POST("/sop-versions/:version_id/publish", server.publishSOPVersion)
	sopManagers.POST("/capture-sops/:sop_id/versions", server.copySOPVersion)
	sopManagers.POST("/sop-versions/:version_id/archive", server.archiveSOPVersion)
	sopManagers.POST("/sop-versions/:version_id/restore", server.restoreSOPVersion)
	sopManagers.POST("/sop-versions/:version_id/views/:view_id/reference-images/upload-url", server.createSOPReferenceUploadURL)
	sopManagers.POST("/sop-versions/:version_id/views/:view_id/reference-images", server.addSOPReferenceImage)
	sopManagers.DELETE("/sop-versions/:version_id/views/:view_id/reference-images/:image_id", server.deleteSOPReferenceImage)
	sopManagers.PUT("/sop-versions/:version_id/views/:view_id/reference-image-order", server.reorderSOPReferenceImages)
	protected.POST("/photo-sessions", server.createPhotoSession)
	protected.POST("/assets/upload-url", server.createUploadURL)
	protected.POST("/assets/complete", server.completeAssetUpload)
	assetReaders := protected.Group("")
	assetReaders.Use(requireRoles(models.RoleSuperAdmin, models.RoleAdmin, models.RoleOperator))
	assetReaders.GET("/assets/:asset_id/media", server.assetMedia)
	assetReaders.GET("/assets/review", server.listAssetsForReview)
	assetReviewManagers := protected.Group("")
	assetReviewManagers.Use(requireRoles(models.RoleSuperAdmin, models.RoleAdmin))
	assetReviewManagers.GET("/assets/review/hierarchy", server.listAssetReviewHierarchy)
	assetReviewers := protected.Group("")
	assetReviewers.Use(requireRoles(models.RoleSuperAdmin, models.RoleAdmin))
	assetReviewers.PATCH("/assets/:asset_id/review", server.reviewAsset)
	userManagers := protected.Group("")
	userManagers.Use(requireRoles(models.RoleSuperAdmin, models.RoleAdmin))
	userManagers.GET("/users", server.listUsers)
	userManagers.POST("/users", server.createUser)
	userManagers.PATCH("/users/:user_id", server.updateUser)
	userManagers.DELETE("/users/:user_id", server.deleteUser)
	userManagers.PUT("/users/:user_id/password", server.resetUserPassword)
}

func registerAIRoutes(protected *gin.RouterGroup, server *Server) {
	aiJobs := protected.Group("")
	aiJobs.Use(requireRoles(models.RoleSuperAdmin, models.RoleAdmin, models.RoleOperator))
	aiJobs.GET("/ai-jobs", server.listAIJobs)
	aiJobs.POST("/ai-jobs", server.createAIJob)
	aiJobs.GET("/ai-jobs/:job_id", server.getAIJob)
	aiJobs.GET("/ai-jobs/:job_id/text-results", server.listAITextResults)
	aiJobs.GET("/ai-jobs/:job_id/image-results", server.listAIImageResults)
	aiJobs.GET("/ai-jobs/:job_id/image-threads", server.listAIImageThreads)
	aiJobs.GET("/ai-jobs/:job_id/image-results/:result_id/media", server.aiImageResultMedia)
	aiJobs.POST("/ai-jobs/:job_id/items/:item_id/image-turns", server.createAIImageTurn)
	aiJobs.POST("/ai-jobs/:job_id/items/:item_id/image-results/:result_id/select", server.selectAIImageResult)
	aiJobs.POST("/ai-jobs/:job_id/items/:item_id/image-results/:result_id/submit-to-assets", server.submitAIImageResultToAssets)
	aiJobs.PATCH("/ai-jobs/:job_id/items/:item_id/text-results/:result_id", server.editAITextResult)
	aiJobs.POST("/ai-jobs/:job_id/items/:item_id/text-results/:result_id/approve", server.approveAITextResult)
	aiJobs.POST("/ai-jobs/:job_id/items/:item_id/text-results/:result_id/reject", server.rejectAITextResult)
	aiJobs.GET("/ai-jobs/:job_id/items/:item_id/text-results/:result_id/application-preview", server.previewAITextResultApplication)
	aiJobs.POST("/ai-jobs/:job_id/items/:item_id/text-results/:result_id/apply", server.applyAITextResult)
	aiJobs.GET("/skus/:sku_id/platform-content", server.getSKUPlatformContent)
	aiJobs.GET("/ai-content-templates", server.listAIContentTemplates)
	aiJobs.GET("/ai-reference-sops", server.listAIReferenceSOPs)
	aiJobs.GET("/ai-reference-sops/:sop_id", server.getAIReferenceSOP)
	aiJobs.GET("/ai-reference-items/:item_id/media", server.aiReferenceItemMedia)
	aiSettings := protected.Group("")
	aiSettings.Use(requireRoles(models.RoleSuperAdmin))
	aiSettings.GET("/settings/openai", server.getOpenAISetting)
	aiSettings.GET("/settings/openai/models", server.listOpenAIModels)
	aiSettings.PATCH("/settings/openai/models", server.updateOpenAIModels)
	aiSettings.PATCH("/settings/openai/workers", server.updateOpenAIWorkers)
	aiSettings.PUT("/settings/openai", server.putOpenAISetting)
	aiSettings.DELETE("/settings/openai", server.disableOpenAISetting)
	aiAdmin := protected.Group("")
	aiAdmin.Use(requireRoles(models.RoleSuperAdmin, models.RoleAdmin))
	aiJobs.GET("/style-reference-grants", server.listStyleReferenceGrants)
	aiAdmin.POST("/style-reference-grants", server.createStyleReferenceGrant)
	aiAdmin.PATCH("/style-reference-grants/:grant_id", server.revokeStyleReferenceGrant)
	aiAdmin.POST("/ai-reference-sops", server.createAIReferenceSOP)
	aiAdmin.POST("/ai-reference-sops/:sop_id/versions", server.copyAIReferenceSOPVersion)
	aiAdmin.GET("/ai-reference-sop-versions/:version_id", server.getAIReferenceVersion)
	aiAdmin.PATCH("/ai-reference-sop-versions/:version_id", server.updateAIReferenceSOPVersion)
	aiAdmin.POST("/ai-reference-sop-versions/:version_id/items/upload-url", server.createAIReferenceItemUploadURL)
	aiAdmin.POST("/ai-reference-sop-versions/:version_id/items/complete", server.completeAIReferenceItemUpload)
	aiAdmin.DELETE("/ai-reference-sop-versions/:version_id/items/:item_id", server.deleteAIReferenceItem)
	aiAdmin.PUT("/ai-reference-sop-versions/:version_id/item-order", server.reorderAIReferenceItems)
	aiAdmin.POST("/ai-reference-sop-versions/:version_id/publish", server.publishAIReferenceSOPVersion)
	aiAdmin.POST("/ai-reference-sop-versions/:version_id/archive", server.archiveAIReferenceSOPVersion)
	aiAdmin.POST("/ai-reference-sop-versions/:version_id/restore", server.restoreAIReferenceSOPVersion)
	aiAdmin.POST("/ai-content-templates", server.createAIContentTemplate)
	aiAdmin.GET("/ai-content-templates/:template_id", server.getAIContentTemplate)
	aiAdmin.POST("/ai-content-templates/:template_id/versions", server.copyAIContentTemplateVersion)
	aiAdmin.PATCH("/ai-content-template-versions/:version_id", server.updateAIContentTemplateVersion)
	aiAdmin.POST("/ai-content-template-versions/:version_id/validate", server.validateAIContentTemplateVersion)
	aiAdmin.POST("/ai-content-template-versions/:version_id/publish", server.publishAIContentTemplateVersion)
	aiAdmin.POST("/ai-content-template-versions/:version_id/archive", server.archiveAIContentTemplateVersion)
	aiAdmin.POST("/ai-content-template-versions/:version_id/restore", server.restoreAIContentTemplateVersion)
	aiAdmin.DELETE("/ai-content-template-versions/:version_id", server.deleteAIContentTemplateDraft)
}

func newAIDependencies(cfg config.Config, db *gorm.DB) (AIDependencies, error) {
	deps := AIDependencies{ProviderSettings: ai.NewProviderSettingsService(db, nil, nil), Templates: ai.NewTemplateService(db), Jobs: ai.NewJobService(db), TextResults: ai.NewTextResultService(db), ImageResults: ai.NewImageResultService(db)}
	key, err := validateSecretsMasterKey(cfg)
	if err != nil {
		return AIDependencies{}, err
	}
	if len(key) == 0 {
		return deps, nil
	}
	box, err := secrets.NewAESGCM(key)
	if err != nil {
		return AIDependencies{}, fmt.Errorf("configure CARGOFLOWS_SECRETS_MASTER_KEY: %w", err)
	}
	deps.ProviderSettings = ai.NewProviderSettingsService(db, box, ai.NewHTTPProviderVerifier(cfg.OpenAIBaseURL, nil))
	return deps, nil
}

func validateSecretsMasterKey(cfg config.Config) ([]byte, error) {
	encoded := strings.TrimSpace(cfg.SecretsMasterKey)
	if encoded == "" {
		if cfg.AppEnv == "production" {
			return nil, fmt.Errorf("CARGOFLOWS_SECRETS_MASTER_KEY is required in production")
		}
		return nil, nil
	}
	key, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("decode CARGOFLOWS_SECRETS_MASTER_KEY: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("CARGOFLOWS_SECRETS_MASTER_KEY must decode to 32 bytes")
	}
	return key, nil
}

func requireRoles(roles ...models.Role) gin.HandlerFunc {
	allowed := make(map[models.Role]struct{}, len(roles))
	for _, role := range roles {
		allowed[role] = struct{}{}
	}
	return func(c *gin.Context) {
		if _, ok := allowed[currentUser(c).Role]; !ok {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": "forbidden", "message": "insufficient permissions"})
			return
		}
		c.Next()
	}
}

func isSOPManager(user models.User) bool {
	return user.Role == models.RoleSuperAdmin || user.Role == models.RoleAdmin || user.Role == models.RoleOperator
}

func isAdministrator(user models.User) bool {
	return user.Role == models.RoleSuperAdmin || user.Role == models.RoleAdmin
}

func (s *Server) requireAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		tokenValue := strings.TrimPrefix(header, "Bearer ")
		if tokenValue == header || tokenValue == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"message": "missing bearer token"})
			return
		}

		claims := jwt.MapClaims{}
		token, err := jwt.ParseWithClaims(tokenValue, claims, func(token *jwt.Token) (any, error) {
			return []byte(s.cfg.JWTSecret), nil
		})
		if err != nil || !token.Valid {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"message": "invalid token"})
			return
		}

		userID, ok := claims["sub"].(float64)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"message": "invalid token subject"})
			return
		}

		var user models.User
		if err := s.db.First(&user, uint(userID)).Error; err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"message": "user not found"})
			return
		}
		if user.Status != "active" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": "account_disabled", "message": "account is disabled"})
			return
		}
		sessionVersion := uint(1)
		if claimed, ok := claims["session_version"].(float64); ok {
			sessionVersion = uint(claimed)
		}
		if sessionVersion != user.SessionVersion {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": "session_revoked", "message": "session has been revoked"})
			return
		}

		c.Set("user", user)
		c.Next()
	}
}

func (s *Server) requirePasswordChanged() gin.HandlerFunc {
	return func(c *gin.Context) {
		if currentUser(c).MustChangePassword {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": "password_change_required", "message": "password change required"})
			return
		}
		c.Next()
	}
}

func currentUser(c *gin.Context) models.User {
	value, _ := c.Get("user")
	user, _ := value.(models.User)
	return user
}

func (s *Server) issueToken(user models.User) (string, error) {
	claims := jwt.MapClaims{
		"sub":             user.ID,
		"role":            user.Role,
		"exp":             time.Now().Add(7 * 24 * time.Hour).Unix(),
		"iat":             time.Now().Unix(),
		"session_version": user.SessionVersion,
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(s.cfg.JWTSecret))
}
