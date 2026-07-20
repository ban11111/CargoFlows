package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"cargoflows/api/internal/ai"
	"cargoflows/api/internal/models"
	"github.com/gin-gonic/gin"
)

func (s *Server) getOpenAISetting(c *gin.Context) {
	if s.ai.ProviderSettings == nil {
		respondAIUnavailable(c)
		return
	}
	value, err := s.ai.ProviderSettings.Get(c.Request.Context())
	if err != nil {
		respondAIError(c, err)
		return
	}
	c.JSON(http.StatusOK, openAISettingDTOFromView(value))
}

func (s *Server) listOpenAIModels(c *gin.Context) {
	if s.ai.ProviderSettings == nil {
		respondAIUnavailable(c)
		return
	}
	values, err := s.ai.ProviderSettings.ListModels(c.Request.Context())
	if err != nil {
		respondAIError(c, err)
		return
	}
	if values == nil {
		values = []ai.ProviderModel{}
	}
	c.JSON(http.StatusOK, gin.H{"data": values})
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
	value, err := s.ai.ProviderSettings.Configure(c.Request.Context(), currentUser(c).ID, *req.APIKey)
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
	value, err := s.ai.ProviderSettings.Disable(c.Request.Context(), currentUser(c).ID)
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
	if !isUUID(req.SKUID) || !isUUID(req.TemplateVersionPublicID) || len(req.SelectedSlotKeys) == 0 || req.SelectedAssetIDs == nil || !allUUIDs(*req.SelectedAssetIDs) || strings.TrimSpace(req.Locale) == "" {
		respondAIBadRequest(c, errors.New("sku_id, a UUID template_version_id, locale, selected_asset_ids array, and at least one selected_slot_key are required"))
		return
	}
	value, err := s.ai.Jobs.Create(c.Request.Context(), ai.CreateJobInput{
		SKUID: req.SKUID, TemplateVersionPublicID: req.TemplateVersionPublicID,
		SelectedSlotKeys: req.SelectedSlotKeys, SelectedAssetIDs: *req.SelectedAssetIDs,
		Locale: req.Locale, CreatedByID: currentUser(c).ID,
		IdempotencyKey: c.GetHeader("Idempotency-Key"), UserPreference: req.UserPreference, GenerationOverrides: req.GenerationOverrides,
		ImageCanvases: req.ImageCanvases,
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
	values, err := s.ai.Jobs.List(c.Request.Context())
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
	value, err := s.ai.Jobs.Get(c.Request.Context(), c.Param("job_id"))
	if err != nil {
		respondAIError(c, err)
		return
	}
	c.JSON(http.StatusOK, value)
}

func (s *Server) listAITextResults(c *gin.Context) {
	if !requireAIUUIDParam(c, "job_id") {
		return
	}
	values, err := s.ai.TextResults.List(c.Request.Context(), c.Param("job_id"))
	if err != nil {
		respondAIError(c, err)
		return
	}
	if values == nil {
		values = []ai.TextResultDocument{}
	}
	c.JSON(http.StatusOK, gin.H{"data": values})
}

func (s *Server) listAIImageResults(c *gin.Context) {
	if !requireAIUUIDParam(c, "job_id") {
		return
	}
	values, err := s.ai.ImageResults.List(c.Request.Context(), c.Param("job_id"))
	if err != nil {
		respondAIError(c, err)
		return
	}
	if values == nil {
		values = []ai.ImageResultDocument{}
	}
	for index := range values {
		values[index].MediaURL = "/api/v1/ai-jobs/" + c.Param("job_id") + "/image-results/" + values[index].PublicID + "/media"
	}
	c.JSON(http.StatusOK, gin.H{"data": values})
}

func (s *Server) aiImageResultMedia(c *gin.Context) {
	if !requireAIUUIDParam(c, "job_id") || !requireAIUUIDParam(c, "result_id") {
		return
	}
	result, err := s.ai.ImageResults.GetForJob(c.Request.Context(), c.Param("job_id"), c.Param("result_id"))
	if errors.Is(err, ai.ErrImageResultNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"code": "not_found", "message": "image result not found"})
		return
	}
	if err != nil {
		respondAIError(c, err)
		return
	}
	reader, ok := s.storage.(interface {
		ReadGenerated(context.Context, string) (ai.ImageInput, error)
	})
	if !ok {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "object_storage_unavailable", "message": "generated image storage is unavailable"})
		return
	}
	source, err := reader.ReadGenerated(c.Request.Context(), result.ObjectKey)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "object_storage_unavailable", "message": "read generated image failed"})
		return
	}
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Disposition", "inline")
	c.Data(http.StatusOK, result.MIMEType, source.Bytes)
}

func (s *Server) editAITextResult(c *gin.Context) {
	if !requireAITextResultParams(c) {
		return
	}
	var req editAITextResultRequest
	if err := decodeJSONStrict(c, &req); err != nil || len(req.Structured) == 0 || !json.Valid(req.Structured) {
		if err == nil {
			err = errors.New("structured must be a JSON object")
		}
		respondAIBadRequest(c, err)
		return
	}
	value, err := s.ai.TextResults.Edit(c.Request.Context(), c.Param("job_id"), c.Param("item_id"), c.Param("result_id"), currentUser(c).ID, req.Structured)
	if err != nil {
		respondAIError(c, err)
		return
	}
	c.JSON(http.StatusOK, value)
}

func (s *Server) approveAITextResult(c *gin.Context) {
	s.mutateAITextResult(c, s.ai.TextResults.Approve)
}

func (s *Server) rejectAITextResult(c *gin.Context) {
	s.mutateAITextResult(c, s.ai.TextResults.Reject)
}

func (s *Server) mutateAITextResult(c *gin.Context, mutate func(context.Context, string, string, string, uint) (ai.TextResultDocument, error)) {
	if !requireAITextResultParams(c) {
		return
	}
	value, err := mutate(c.Request.Context(), c.Param("job_id"), c.Param("item_id"), c.Param("result_id"), currentUser(c).ID)
	if err != nil {
		respondAIError(c, err)
		return
	}
	c.JSON(http.StatusOK, value)
}

func (s *Server) previewAITextResultApplication(c *gin.Context) {
	if !requireAITextResultParams(c) {
		return
	}
	value, err := s.ai.TextResults.Preview(c.Request.Context(), c.Param("job_id"), c.Param("item_id"), c.Param("result_id"))
	if err != nil {
		respondAIError(c, err)
		return
	}
	c.JSON(http.StatusOK, value)
}

func (s *Server) applyAITextResult(c *gin.Context) {
	if !requireAITextResultParams(c) {
		return
	}
	value, err := s.ai.TextResults.Apply(c.Request.Context(), c.Param("job_id"), c.Param("item_id"), c.Param("result_id"), currentUser(c).ID)
	if err != nil {
		respondAIError(c, err)
		return
	}
	c.JSON(http.StatusOK, value)
}

func (s *Server) getSKUPlatformContent(c *gin.Context) {
	platform, locale := strings.TrimSpace(c.Query("platform")), strings.TrimSpace(c.Query("locale"))
	if !isUUID(c.Param("sku_id")) || platform == "" || locale == "" {
		respondAIBadRequest(c, errors.New("sku_id, platform, and locale are required"))
		return
	}
	value, err := s.ai.TextResults.GetPlatformContent(c.Request.Context(), c.Param("sku_id"), platform, locale)
	if err != nil {
		respondAIError(c, err)
		return
	}
	c.JSON(http.StatusOK, value)
}

func requireAITextResultParams(c *gin.Context) bool {
	return requireAIUUIDParam(c, "job_id") && requireAIUUIDParam(c, "item_id") && requireAIUUIDParam(c, "result_id")
}

func (s *Server) listAIContentTemplates(c *gin.Context) {
	includeAll, err := strconv.ParseBool(defaultString(c.Query("include_all"), "false"))
	if err != nil {
		respondAIBadRequest(c, errors.New("include_all must be a boolean"))
		return
	}
	if includeAll && !isAdministrator(currentUser(c)) {
		c.JSON(http.StatusForbidden, gin.H{"code": "forbidden", "message": "insufficient permissions"})
		return
	}
	values, err := s.ai.Templates.List(c.Request.Context(), includeAll)
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
	created, err := s.ai.Templates.Create(c.Request.Context(), ai.CreateTemplateInput{
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
	value, err := s.ai.Templates.Get(c.Request.Context(), c.Param("template_id"))
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
	value, err := s.ai.Templates.UpdateDraft(c.Request.Context(), c.Param("version_id"), templateMutationInput(req))
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
	issues, err := s.ai.Templates.Validate(c.Request.Context(), c.Param("version_id"))
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
	issues, err := s.ai.Templates.Publish(c.Request.Context(), c.Param("version_id"), currentUser(c).ID)
	if err != nil {
		respondAIError(c, err)
		return
	}
	if len(issues) != 0 {
		c.JSON(http.StatusUnprocessableEntity, templateValidationDTO(issues))
		return
	}
	value, err := s.ai.Templates.GetVersion(c.Request.Context(), c.Param("version_id"))
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
	value, err := s.ai.Templates.CopyVersion(c.Request.Context(), c.Param("template_id"), *req.SourceVersionID, currentUser(c).ID)
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
	if err := s.ai.Templates.Archive(c.Request.Context(), c.Param("version_id")); err != nil {
		respondAIError(c, err)
		return
	}
	value, err := s.ai.Templates.GetVersion(c.Request.Context(), c.Param("version_id"))
	if err != nil {
		respondAIError(c, err)
		return
	}
	c.JSON(http.StatusOK, aiContentTemplateVersionDTOFromModel(*value))
}

func (s *Server) restoreAIContentTemplateVersion(c *gin.Context) {
	if !requireAIUUIDParam(c, "version_id") {
		return
	}
	if err := s.ai.Templates.Restore(c.Request.Context(), c.Param("version_id")); err != nil {
		respondAIError(c, err)
		return
	}
	value, err := s.ai.Templates.GetVersion(c.Request.Context(), c.Param("version_id"))
	if err != nil {
		respondAIError(c, err)
		return
	}
	c.JSON(http.StatusOK, aiContentTemplateVersionDTOFromModel(*value))
}

func (s *Server) deleteAIContentTemplateDraft(c *gin.Context) {
	if !requireAIUUIDParam(c, "version_id") {
		return
	}
	if err := s.ai.Templates.DeleteDraft(c.Request.Context(), c.Param("version_id")); err != nil {
		respondAIError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
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
	case errors.Is(err, ai.ErrProviderNotActive):
		c.JSON(http.StatusConflict, gin.H{"code": "provider_not_active", "message": err.Error()})
	case errors.Is(err, ai.ErrProviderModelsUnavailable):
		c.JSON(http.StatusBadGateway, gin.H{"code": "provider_models_unavailable", "message": "OpenAI model list is unavailable"})
	case errors.Is(err, ai.ErrTemplateNotFound), errors.Is(err, ai.ErrTemplateVersionNotFound):
		c.JSON(http.StatusNotFound, gin.H{"code": "not_found", "message": err.Error()})
	case errors.Is(err, ai.ErrJobNotFound), errors.Is(err, ai.ErrSKUNotFound):
		c.JSON(http.StatusNotFound, gin.H{"code": "not_found", "message": err.Error()})
	case errors.Is(err, ai.ErrTextResultNotFound):
		c.JSON(http.StatusNotFound, gin.H{"code": "not_found", "message": err.Error()})
	case errors.Is(err, ai.ErrTextResultInvalid):
		c.JSON(http.StatusUnprocessableEntity, gin.H{"code": "text_result_invalid", "message": err.Error()})
	case errors.Is(err, ai.ErrTextResultLifecycleConflict), errors.Is(err, ai.ErrTextResultApprovalRequired), errors.Is(err, ai.ErrTextResultNotEffective):
		c.JSON(http.StatusConflict, gin.H{"code": "lifecycle_conflict", "message": err.Error()})
	case errors.Is(err, ai.ErrSlotSelectionInvalid):
		respondAIBadRequest(c, err)
	case errors.Is(err, ai.ErrIdempotencyKeyInvalid), errors.Is(err, ai.ErrLocaleInvalid), errors.Is(err, ai.ErrUserPreferenceInvalid), errors.Is(err, ai.ErrUserPreferenceNotAllowed), errors.Is(err, ai.ErrGenerationOverrideInvalid), errors.Is(err, ai.ErrTemplateVersionIDInvalid), errors.Is(err, ai.ErrSKUIDInvalid), errors.Is(err, ai.ErrAssetIDInvalid):
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
