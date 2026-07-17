package app

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"cargoflow/api/internal/ai"
	"cargoflow/api/internal/models"
	"github.com/gin-gonic/gin"
)

func (s *Server) getOpenAISetting(c *gin.Context) {
	if s.ai.ProviderSettings == nil {
		respondAIUnavailable(c)
		return
	}
	value, err := s.ai.ProviderSettings.Get(c)
	if err != nil {
		respondAIError(c, err)
		return
	}
	c.JSON(http.StatusOK, openAISettingDTOFromView(value))
}

func (s *Server) putOpenAISetting(c *gin.Context) {
	if s.ai.ProviderSettings == nil {
		respondAIUnavailable(c)
		return
	}
	var req openAISettingRequest
	if err := decodeJSONStrict(c, &req); err != nil || req.APIKey == nil || strings.TrimSpace(*req.APIKey) == "" {
		if err == nil {
			err = errors.New("api_key is required")
		}
		respondAIBadRequest(c, err)
		return
	}
	value, err := s.ai.ProviderSettings.Configure(c, currentUser(c).ID, *req.APIKey)
	if err != nil {
		respondAIError(c, err)
		return
	}
	c.JSON(http.StatusOK, openAISettingDTOFromView(value))
}

func (s *Server) disableOpenAISetting(c *gin.Context) {
	if s.ai.ProviderSettings == nil {
		respondAIUnavailable(c)
		return
	}
	value, err := s.ai.ProviderSettings.Disable(c, currentUser(c).ID)
	if err != nil {
		respondAIError(c, err)
		return
	}
	c.JSON(http.StatusOK, openAISettingDTOFromView(value))
}

func (s *Server) createAIJob(c *gin.Context) {
	var req createAIJobRequest
	if err := decodeJSONStrict(c, &req); err != nil {
		respondAIBadRequest(c, err)
		return
	}
	if req.SKUID == 0 || !isUUID(req.TemplateVersionPublicID) || len(req.SelectedSlotKeys) == 0 || req.SelectedAssetIDs == nil || strings.TrimSpace(req.Locale) == "" {
		respondAIBadRequest(c, errors.New("sku_id, a UUID template_version_id, locale, selected_asset_ids array, and at least one selected_slot_key are required"))
		return
	}
	value, err := s.ai.Jobs.Create(c, ai.CreateJobInput{
		SKUID: req.SKUID, TemplateVersionPublicID: req.TemplateVersionPublicID,
		SelectedSlotKeys: req.SelectedSlotKeys, SelectedAssetIDs: *req.SelectedAssetIDs,
		Locale: req.Locale, CreatedByID: currentUser(c).ID,
		IdempotencyKey: c.GetHeader("Idempotency-Key"), UserPreference: req.UserPreference, GenerationOverrides: req.GenerationOverrides,
	})
	if err != nil {
		respondAIError(c, err)
		return
	}
	status := http.StatusCreated
	if value.Replayed {
		status = http.StatusOK
	}
	c.JSON(status, value)
}

func (s *Server) listAIJobs(c *gin.Context) {
	values, err := s.ai.Jobs.List(c)
	if err != nil {
		respondAIError(c, err)
		return
	}
	if values == nil {
		values = []ai.JobDocument{}
	}
	c.JSON(http.StatusOK, gin.H{"data": values})
}

func (s *Server) getAIJob(c *gin.Context) {
	if !requireAIUUIDParam(c, "job_id") {
		return
	}
	value, err := s.ai.Jobs.Get(c, c.Param("job_id"))
	if err != nil {
		respondAIError(c, err)
		return
	}
	c.JSON(http.StatusOK, value)
}

func (s *Server) listAIContentTemplates(c *gin.Context) {
	includeAll, err := strconv.ParseBool(defaultString(c.Query("include_all"), "false"))
	if err != nil {
		respondAIBadRequest(c, errors.New("include_all must be a boolean"))
		return
	}
	if includeAll && currentUser(c).Role != models.RoleAdmin {
		c.JSON(http.StatusForbidden, gin.H{"code": "forbidden", "message": "insufficient permissions"})
		return
	}
	values, err := s.ai.Templates.List(c, includeAll)
	if err != nil {
		respondAIError(c, err)
		return
	}
	data := make([]aiContentTemplateDTO, 0, len(values))
	for _, value := range values {
		data = append(data, aiContentTemplateDTOFromModel(value))
	}
	c.JSON(http.StatusOK, gin.H{"data": data})
}

func (s *Server) createAIContentTemplate(c *gin.Context) {
	var req aiContentTemplateMutationRequest
	if err := decodeJSONStrict(c, &req); err != nil {
		respondAIBadRequest(c, err)
		return
	}
	versionInput := templateMutationInput(req)
	created, err := s.ai.Templates.Create(c, ai.CreateTemplateInput{
		NameZH: req.NameZH, NameEN: req.NameEN, TargetPlatform: req.TargetPlatform,
		CreatedByID: currentUser(c).ID, Version: versionInput,
	})
	if err != nil {
		respondAIError(c, err)
		return
	}
	created.Template.Versions = []models.AIContentTemplateVersion{created.Version}
	c.JSON(http.StatusCreated, aiContentTemplateDTOFromModel(created.Template))
}

func (s *Server) getAIContentTemplate(c *gin.Context) {
	if !requireAIUUIDParam(c, "template_id") {
		return
	}
	value, err := s.ai.Templates.Get(c, c.Param("template_id"))
	if err != nil {
		respondAIError(c, err)
		return
	}
	c.JSON(http.StatusOK, aiContentTemplateDTOFromModel(*value))
}

func (s *Server) updateAIContentTemplateVersion(c *gin.Context) {
	if !requireAIUUIDParam(c, "version_id") {
		return
	}
	var req aiContentTemplateMutationRequest
	if err := decodeJSONStrict(c, &req); err != nil {
		respondAIBadRequest(c, err)
		return
	}
	value, err := s.ai.Templates.UpdateDraft(c, c.Param("version_id"), templateMutationInput(req))
	if err != nil {
		respondAIError(c, err)
		return
	}
	c.JSON(http.StatusOK, aiContentTemplateVersionDTOFromModel(*value))
}

func (s *Server) validateAIContentTemplateVersion(c *gin.Context) {
	if !requireAIUUIDParam(c, "version_id") {
		return
	}
	issues, err := s.ai.Templates.Validate(c, c.Param("version_id"))
	if err != nil {
		respondAIError(c, err)
		return
	}
	c.JSON(http.StatusOK, templateValidationDTO(issues))
}

func (s *Server) publishAIContentTemplateVersion(c *gin.Context) {
	if !requireAIUUIDParam(c, "version_id") {
		return
	}
	issues, err := s.ai.Templates.Publish(c, c.Param("version_id"), currentUser(c).ID)
	if err != nil {
		respondAIError(c, err)
		return
	}
	if len(issues) != 0 {
		c.JSON(http.StatusUnprocessableEntity, templateValidationDTO(issues))
		return
	}
	value, err := s.ai.Templates.GetVersion(c, c.Param("version_id"))
	if err != nil {
		respondAIError(c, err)
		return
	}
	c.JSON(http.StatusOK, aiContentTemplateVersionDTOFromModel(*value))
}

func (s *Server) copyAIContentTemplateVersion(c *gin.Context) {
	if !requireAIUUIDParam(c, "template_id") {
		return
	}
	var req copyAIContentTemplateVersionRequest
	if err := decodeJSONStrict(c, &req); err != nil || req.SourceVersionID == nil || !isUUID(*req.SourceVersionID) {
		if err == nil {
			err = errors.New("source_version_id must be a UUID")
		}
		respondAIBadRequest(c, err)
		return
	}
	value, err := s.ai.Templates.CopyVersion(c, c.Param("template_id"), *req.SourceVersionID, currentUser(c).ID)
	if err != nil {
		respondAIError(c, err)
		return
	}
	c.JSON(http.StatusCreated, aiContentTemplateVersionDTOFromModel(*value))
}

func (s *Server) archiveAIContentTemplateVersion(c *gin.Context) {
	if !requireAIUUIDParam(c, "version_id") {
		return
	}
	if err := s.ai.Templates.Archive(c, c.Param("version_id")); err != nil {
		respondAIError(c, err)
		return
	}
	value, err := s.ai.Templates.GetVersion(c, c.Param("version_id"))
	if err != nil {
		respondAIError(c, err)
		return
	}
	c.JSON(http.StatusOK, aiContentTemplateVersionDTOFromModel(*value))
}

func templateValidationDTO(issues []ai.ValidationIssue) aiTemplateValidationDTO {
	if issues == nil {
		issues = []ai.ValidationIssue{}
	}
	code := "template_valid"
	if len(issues) != 0 {
		code = "template_validation_failed"
	}
	return aiTemplateValidationDTO{Code: code, Issues: issues}
}

func respondAIError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ai.ErrInvalidAPIKey):
		respondAIBadRequest(c, err)
	case errors.Is(err, ai.ErrCredentialVerification):
		c.JSON(http.StatusUnprocessableEntity, gin.H{"code": "credential_verification_failed", "message": "OpenAI credential verification failed"})
	case errors.Is(err, ai.ErrProviderNotConfigured):
		c.JSON(http.StatusNotFound, gin.H{"code": "provider_not_configured", "message": err.Error()})
	case errors.Is(err, ai.ErrTemplateNotFound), errors.Is(err, ai.ErrTemplateVersionNotFound):
		c.JSON(http.StatusNotFound, gin.H{"code": "not_found", "message": err.Error()})
	case errors.Is(err, ai.ErrJobNotFound), errors.Is(err, ai.ErrSKUNotFound):
		c.JSON(http.StatusNotFound, gin.H{"code": "not_found", "message": err.Error()})
	case errors.Is(err, ai.ErrSlotSelectionInvalid):
		respondAIBadRequest(c, err)
	case errors.Is(err, ai.ErrIdempotencyKeyInvalid), errors.Is(err, ai.ErrLocaleInvalid), errors.Is(err, ai.ErrUserPreferenceInvalid), errors.Is(err, ai.ErrUserPreferenceNotAllowed), errors.Is(err, ai.ErrGenerationOverrideInvalid), errors.Is(err, ai.ErrTemplateVersionIDInvalid):
		respondAIBadRequest(c, err)
	case errors.Is(err, ai.ErrPublishedTemplateConfigInvalid):
		c.JSON(http.StatusInternalServerError, gin.H{"code": "ai_template_configuration_invalid", "message": "Published AI template configuration is invalid"})
	case errors.Is(err, ai.ErrIdempotencyConflict):
		c.JSON(http.StatusConflict, gin.H{"code": "idempotency_conflict", "message": err.Error()})
	case errors.Is(err, ai.ErrAssetNotEligible), errors.Is(err, ai.ErrPublishedSOPNotFound):
		c.JSON(http.StatusUnprocessableEntity, gin.H{"code": "job_input_not_eligible", "message": err.Error()})
	case errors.Is(err, ai.ErrTemplateVersionImmutable), errors.Is(err, ai.ErrTemplateDraftExists), errors.Is(err, ai.ErrTemplateSourceNotPublished):
		c.JSON(http.StatusConflict, gin.H{"code": "lifecycle_conflict", "message": err.Error()})
	case errors.Is(err, ai.ErrTemplateVersionNotPublished):
		c.JSON(http.StatusConflict, gin.H{"code": "lifecycle_conflict", "message": err.Error()})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "unexpected server error"})
	}
}

func respondAIBadRequest(c *gin.Context, err error) {
	c.JSON(http.StatusBadRequest, gin.H{"code": "invalid_request", "message": err.Error()})
}

func respondAIUnavailable(c *gin.Context) {
	c.JSON(http.StatusServiceUnavailable, gin.H{"code": "ai_not_configured", "message": "AI credential encryption is not configured"})
}

func requireAIUUIDParam(c *gin.Context, name string) bool {
	if !isUUID(c.Param(name)) {
		respondAIBadRequest(c, errors.New(name+" must be a UUID"))
		return false
	}
	return true
}

func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
