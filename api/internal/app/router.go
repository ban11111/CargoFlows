package app

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"time"

	"cargoflow/api/internal/ai"
	"cargoflow/api/internal/config"
	"cargoflow/api/internal/models"
	"cargoflow/api/internal/secrets"
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
	registerExistingRoutes(protected, server)
	registerAIRoutes(protected, server)

	return router
}

func registerExistingRoutes(protected *gin.RouterGroup, server *Server) {
	protected.GET("/skus", server.listSKUs)
	protected.POST("/skus", server.createSKU)
	protected.GET("/skus/:id", server.getSKU)
	protected.PATCH("/skus/:id", server.updateSKU)
	protected.POST("/skus/:id/inventory-adjustments", server.createInventoryAdjustment)
	protected.GET("/skus/:id/inventory-history", server.listInventoryHistory)
	protected.GET("/categories", server.listCategories)
	protected.POST("/categories", server.createCategory)
	protected.DELETE("/categories/:id", server.deleteCategory)
	protected.GET("/tags", server.listTags)
	protected.GET("/capture-sops", server.listCaptureSOPs)
	protected.GET("/capture-sops/:sop_id", server.getCaptureSOP)
	protected.GET("/sop-versions/:version_id", server.getSOPVersion)
	sopManagers := protected.Group("")
	sopManagers.Use(requireRoles(models.RoleAdmin, models.RoleOperator))
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
	sopManagers.POST("/sop-versions/:version_id/views/:view_id/reference-images/upload-url", server.createSOPReferenceUploadURL)
	sopManagers.POST("/sop-versions/:version_id/views/:view_id/reference-images", server.addSOPReferenceImage)
	sopManagers.DELETE("/sop-versions/:version_id/views/:view_id/reference-images/:image_id", server.deleteSOPReferenceImage)
	sopManagers.PUT("/sop-versions/:version_id/views/:view_id/reference-image-order", server.reorderSOPReferenceImages)
	protected.POST("/photo-sessions", server.createPhotoSession)
	protected.POST("/assets/upload-url", server.createUploadURL)
	protected.POST("/assets/complete", server.completeAssetUpload)
	protected.GET("/assets/review", server.listAssetsForReview)
	protected.GET("/assets/review/hierarchy", server.listAssetReviewHierarchy)
	protected.PATCH("/assets/:id/review", server.reviewAsset)
	protected.GET("/users", server.listUsers)
}

func registerAIRoutes(protected *gin.RouterGroup, server *Server) {
	aiJobs := protected.Group("")
	aiJobs.Use(requireRoles(models.RoleAdmin, models.RoleOperator))
	aiJobs.GET("/ai-jobs", server.listAIJobs)
	aiJobs.POST("/ai-jobs", server.createAIJob)
	aiJobs.GET("/ai-jobs/:job_id", server.getAIJob)
	aiJobs.GET("/ai-jobs/:job_id/text-results", server.listAITextResults)
	aiJobs.PATCH("/ai-jobs/:job_id/items/:item_id/text-results/:result_id", server.editAITextResult)
	aiJobs.POST("/ai-jobs/:job_id/items/:item_id/text-results/:result_id/approve", server.approveAITextResult)
	aiJobs.POST("/ai-jobs/:job_id/items/:item_id/text-results/:result_id/reject", server.rejectAITextResult)
	aiJobs.GET("/ai-jobs/:job_id/items/:item_id/text-results/:result_id/application-preview", server.previewAITextResultApplication)
	aiJobs.POST("/ai-jobs/:job_id/items/:item_id/text-results/:result_id/apply", server.applyAITextResult)
	aiJobs.GET("/skus/:id/platform-content", server.getSKUPlatformContent)
	aiJobs.GET("/ai-content-templates", server.listAIContentTemplates)
	aiAdmin := protected.Group("")
	aiAdmin.Use(requireRoles(models.RoleAdmin))
	aiAdmin.GET("/settings/openai", server.getOpenAISetting)
	aiAdmin.PUT("/settings/openai", server.putOpenAISetting)
	aiAdmin.DELETE("/settings/openai", server.disableOpenAISetting)
	aiAdmin.POST("/ai-content-templates", server.createAIContentTemplate)
	aiAdmin.GET("/ai-content-templates/:template_id", server.getAIContentTemplate)
	aiAdmin.POST("/ai-content-templates/:template_id/versions", server.copyAIContentTemplateVersion)
	aiAdmin.PATCH("/ai-content-template-versions/:version_id", server.updateAIContentTemplateVersion)
	aiAdmin.POST("/ai-content-template-versions/:version_id/validate", server.validateAIContentTemplateVersion)
	aiAdmin.POST("/ai-content-template-versions/:version_id/publish", server.publishAIContentTemplateVersion)
	aiAdmin.POST("/ai-content-template-versions/:version_id/archive", server.archiveAIContentTemplateVersion)
}

func newAIDependencies(cfg config.Config, db *gorm.DB) (AIDependencies, error) {
	deps := AIDependencies{Templates: ai.NewTemplateService(db), Jobs: ai.NewJobService(db), TextResults: ai.NewTextResultService(db)}
	key, err := validateSecretsMasterKey(cfg)
	if err != nil {
		return AIDependencies{}, err
	}
	if len(key) == 0 {
		return deps, nil
	}
	box, err := secrets.NewAESGCM(key)
	if err != nil {
		return AIDependencies{}, fmt.Errorf("configure CARGOFLOW_SECRETS_MASTER_KEY: %w", err)
	}
	deps.ProviderSettings = ai.NewProviderSettingsService(db, box, ai.NewHTTPProviderVerifier(cfg.OpenAIBaseURL, nil))
	return deps, nil
}

func validateSecretsMasterKey(cfg config.Config) ([]byte, error) {
	encoded := strings.TrimSpace(cfg.SecretsMasterKey)
	if encoded == "" {
		if cfg.AppEnv == "production" {
			return nil, fmt.Errorf("CARGOFLOW_SECRETS_MASTER_KEY is required in production")
		}
		return nil, nil
	}
	key, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("decode CARGOFLOW_SECRETS_MASTER_KEY: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("CARGOFLOW_SECRETS_MASTER_KEY must decode to 32 bytes")
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
	return user.Role == models.RoleAdmin || user.Role == models.RoleOperator
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

		c.Set("user", user)
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
		"sub":  user.ID,
		"role": user.Role,
		"exp":  time.Now().Add(7 * 24 * time.Hour).Unix(),
		"iat":  time.Now().Unix(),
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(s.cfg.JWTSecret))
}
