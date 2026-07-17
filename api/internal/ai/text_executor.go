package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"cargoflow/api/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrRealImageGenerationUnsupported = errors.New("real image generation is not available in this phase")
	ErrExecutionNeedsAttention        = errors.New("AI execution needs operator attention")
)

type ActiveCredentialSource interface {
	DecryptActiveCredential(context.Context) (ActiveOpenAICredential, error)
}

type TextExecutorConfig struct {
	Model           string
	ReasoningEffort string
}

type TextExecutor struct {
	db          *gorm.DB
	credentials ActiveCredentialSource
	provider    TextProvider
	config      TextExecutorConfig
	clock       Clock
}

func NewTextExecutor(db *gorm.DB, credentials ActiveCredentialSource, provider TextProvider, config TextExecutorConfig) *TextExecutor {
	return newTextExecutorWithClock(db, credentials, provider, config, SystemClock{})
}

func newTextExecutorWithClock(db *gorm.DB, credentials ActiveCredentialSource, provider TextProvider, config TextExecutorConfig, clock Clock) *TextExecutor {
	if config.Model == "" {
		config.Model = "gpt-5.6-terra"
	}
	if config.ReasoningEffort == "" {
		config.ReasoningEffort = "low"
	}
	if clock == nil {
		clock = SystemClock{}
	}
	return &TextExecutor{db: db, credentials: credentials, provider: provider, config: config, clock: clock}
}

func (executor *TextExecutor) Execute(ctx context.Context, leased LeasedItem) error {
	prepared, err := executor.prepare(ctx, leased)
	if err != nil {
		return err
	}
	if prepared.completed {
		return nil
	}
	if prepared.recoverStored {
		return executor.finalize(ctx, prepared.execution, prepared.execution.ProviderOutputJSON)
	}

	credential, err := executor.credentials.DecryptActiveCredential(ctx)
	defer clearBytes(credential.APIKey)
	if err != nil {
		persistErr := executor.markProviderFailure(ctx, prepared.execution.ID, models.AIExecutionFailed, "OpenAI credential is unavailable", "")
		return errors.Join(err, persistErr)
	}
	if len(credential.APIKey) == 0 || credential.SettingID == 0 || credential.KeyFingerprint == "" {
		persistErr := executor.markProviderFailure(ctx, prepared.execution.ID, models.AIExecutionFailed, "OpenAI credential is unavailable", "")
		return errors.Join(ErrProviderNotActive, persistErr)
	}
	if err := executor.dispatch(ctx, leased, prepared, credential); err != nil {
		if errors.Is(err, ErrLeaseLost) {
			return err
		}
		persistErr := executor.markProviderFailure(ctx, prepared.execution.ID, models.AIExecutionFailed, "OpenAI dispatch failed before a confirmed call", "")
		return errors.Join(err, persistErr)
	}
	response, providerErr := executor.provider.Generate(ctx, credential.APIKey, TextRequest{
		Prompt: prepared.prompt,
		Metadata: map[string]string{
			"job_id":       prepared.jobPublicID,
			"job_item_id":  prepared.itemPublicID,
			"execution_id": prepared.execution.PublicID,
		},
	})
	clearBytes(credential.APIKey)
	if providerErr != nil {
		status, safeError := providerFailureState(providerErr)
		persistErr := executor.markProviderFailure(ctx, prepared.execution.ID, status, safeError, providerRequestID(providerErr))
		return errors.Join(providerErr, persistErr)
	}
	if !validExecutorTextResponse(response, prepared.prompt) {
		persistErr := executor.markProviderFailure(ctx, prepared.execution.ID, models.AIExecutionFailed, "OpenAI returned an invalid text response", response.RequestID)
		return errors.Join(ErrTextProviderInvalidResponse, persistErr)
	}
	if err := executor.captureProviderResponse(ctx, prepared.execution.ID, response); err != nil {
		persistErr := executor.markProviderFailure(ctx, prepared.execution.ID, models.AIExecutionNeedsAttention, "OpenAI response could not be safely stored", response.RequestID)
		return errors.Join(err, persistErr)
	}
	prepared.execution.ProviderOutputJSON = append([]byte(nil), response.OutputJSON...)
	prepared.execution.OpenAIResponseID = response.ResponseID
	prepared.execution.OpenAIRequestID = response.RequestID
	prepared.execution.Model = response.Model
	prepared.execution.InputTextTokens = response.Usage.InputTextTokens
	prepared.execution.OutputTextTokens = response.Usage.OutputTextTokens
	return executor.finalize(ctx, prepared.execution, response.OutputJSON)
}

type preparedTextExecution struct {
	execution      models.AIExecution
	prompt         CompiledTextPrompt
	jobPublicID    string
	itemPublicID   string
	completed      bool
	recoverStored  bool
	needsAttention bool
}

func (executor *TextExecutor) prepare(ctx context.Context, leased LeasedItem) (preparedTextExecution, error) {
	var prepared preparedTextExecution
	err := executor.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		item, job, version, relationalSlot, err := loadExecutionBindings(tx, leased, executor.clock.Now())
		if err != nil {
			return err
		}
		if item.Kind != models.AIContentSlotTitle && item.Kind != models.AIContentSlotSEODescription {
			return ErrRealImageGenerationUnsupported
		}
		if _, err := validateDryRunProvenance(job, item, version, relationalSlot); err != nil {
			return err
		}
		var snapshot ProductSnapshotV1
		var slot SlotFacts
		if decodeStrictJSON(job.InputSnapshotJSON, &snapshot) != nil || decodeStrictJSON(item.SlotSnapshotJSON, &slot) != nil {
			return invalidExecutionInput("malformed text prompt snapshot")
		}
		prompt, err := CompileTextPrompt(snapshot, slot)
		if err != nil {
			return invalidExecutionInput("text prompt compilation failed")
		}

		var existing models.AIExecution
		err = tx.Where("ai_job_item_id = ?", item.ID).Order("attempt_number DESC, id DESC").First(&existing).Error
		if err == nil {
			prepared = preparedTextExecution{execution: existing, prompt: prompt, jobPublicID: job.PublicID, itemPublicID: item.PublicID}
			if existing.CompiledPromptSHA256 != prompt.SHA256 {
				return invalidExecutionInput("compiled prompt changed during recovery")
			}
			switch existing.Status {
			case models.AIExecutionCompleted:
				prepared.completed = true
				return nil
			case models.AIExecutionStoring:
				if len(existing.ProviderOutputJSON) == 0 {
					return ErrExecutionNeedsAttention
				}
				prepared.recoverStored = true
				return nil
			case models.AIExecutionPreparing:
			case models.AIExecutionCallingOpenAI:
				prepared.needsAttention = true
				return nil
			default:
				return ErrExecutionNeedsAttention
			}
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		} else {
			requestConfig, marshalErr := json.Marshal(map[string]any{"model": executor.config.Model, "reasoning_effort": executor.config.ReasoningEffort, "candidate_count": prompt.CandidateCount, "schema_name": prompt.SchemaName, "store": false})
			if marshalErr != nil {
				return marshalErr
			}
			now := executor.clock.Now()
			existing = models.AIExecution{
				PublicID: uuid.NewString(), AIJobItemID: item.ID, Operation: models.AIExecutionTextGenerate, Status: models.AIExecutionPreparing,
				AttemptNumber: item.AttemptCount, L0PolicyVersion: prompt.LayerVersions.L0, L1ProductContextVersion: prompt.LayerVersions.L1,
				L2TemplateVersionPublicID: prompt.LayerVersions.L2, L3ContentSlotPublicID: prompt.LayerVersions.L3,
				NormalizedInputJSON: prompt.InputJSON, OrderedInputListJSON: item.SelectedInputAssetIDsJSON,
				CompiledPrompt: prompt.Instructions, CompiledPromptSHA256: prompt.SHA256, UserInstruction: snapshot.UserPreference,
				Model: executor.config.Model, RequestConfigJSON: requestConfig, WorkerID: leased.LeaseOwner, LeaseExpiresAt: item.LeaseExpiresAt, StartedAt: &now,
			}
			if createErr := tx.Create(&existing).Error; createErr != nil {
				return createErr
			}
			prepared = preparedTextExecution{execution: existing, prompt: prompt, jobPublicID: job.PublicID, itemPublicID: item.PublicID}
		}
		return nil
	})
	if err == nil && prepared.needsAttention {
		persistErr := executor.markProviderFailure(ctx, prepared.execution.ID, models.AIExecutionNeedsAttention, "OpenAI call outcome is ambiguous", prepared.execution.OpenAIRequestID)
		return prepared, errors.Join(ErrExecutionNeedsAttention, persistErr)
	}
	return prepared, err
}

func (executor *TextExecutor) dispatch(ctx context.Context, leased LeasedItem, prepared preparedTextExecution, credential ActiveOpenAICredential) error {
	return executor.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		item, _, _, _, err := loadExecutionBindings(tx, leased, executor.clock.Now())
		if err != nil {
			return err
		}
		now := executor.clock.Now()
		settingUpdate := tx.Model(&models.OpenAIProviderSetting{}).Where("id = ? AND provider = ? AND status = ? AND key_fingerprint = ?", credential.SettingID, openAIProvider, "active", credential.KeyFingerprint).Update("last_used_at", now)
		if settingUpdate.Error != nil {
			return settingUpdate.Error
		}
		if settingUpdate.RowsAffected != 1 {
			return ErrProviderNotActive
		}
		executionUpdate := tx.Model(&models.AIExecution{}).Where("id = ? AND status = ?", prepared.execution.ID, models.AIExecutionPreparing).Updates(map[string]any{
			"status": models.AIExecutionCallingOpenAI, "openai_provider_setting_id": credential.SettingID, "openai_key_fingerprint": credential.KeyFingerprint,
		})
		if executionUpdate.Error != nil {
			return executionUpdate.Error
		}
		if executionUpdate.RowsAffected != 1 {
			return ErrExecutionNeedsAttention
		}
		metadata, err := json.Marshal(map[string]any{"model": executor.config.Model, "key_fingerprint": credential.KeyFingerprint, "attempt_number": prepared.execution.AttemptNumber})
		if err != nil {
			return err
		}
		jobID, itemID, executionID := item.AIJobID, item.ID, prepared.execution.ID
		audit := models.AIAuditEvent{PublicID: uuid.NewString(), EventType: "ai_execution.text_dispatched", EntityType: "ai_execution", EntityPublicID: prepared.execution.PublicID, AIJobID: &jobID, AIJobItemID: &itemID, AIExecutionID: &executionID, MetadataJSON: metadata}
		return tx.Create(&audit).Error
	})
}

func loadExecutionBindings(tx *gorm.DB, leased LeasedItem, now time.Time) (models.AIJobItem, models.AIJob, models.AIContentTemplateVersion, models.AIContentSlot, error) {
	if leased.itemID == 0 || leased.LeaseOwner == "" {
		return models.AIJobItem{}, models.AIJob{}, models.AIContentTemplateVersion{}, models.AIContentSlot{}, ErrInvalidLease
	}
	query := tx
	if tx.Dialector.Name() == "mysql" {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var item models.AIJobItem
	if err := query.First(&item, leased.itemID).Error; err != nil {
		return item, models.AIJob{}, models.AIContentTemplateVersion{}, models.AIContentSlot{}, err
	}
	if item.Status != models.AIJobItemRunning || item.LeaseOwner != leased.LeaseOwner || item.AttemptCount != leased.Attempt || item.LeaseExpiresAt == nil || !item.LeaseExpiresAt.After(now) {
		return item, models.AIJob{}, models.AIContentTemplateVersion{}, models.AIContentSlot{}, ErrLeaseLost
	}
	var job models.AIJob
	var version models.AIContentTemplateVersion
	var slot models.AIContentSlot
	if err := tx.First(&job, item.AIJobID).Error; err != nil {
		return item, job, version, slot, err
	}
	if err := tx.Select("id", "public_id").First(&version, job.AIContentTemplateVersionID).Error; err != nil {
		return item, job, version, slot, err
	}
	if err := tx.Select("id", "public_id", "ai_content_template_version_id", "slot_key", "kind").First(&slot, item.AIContentSlotID).Error; err != nil {
		return item, job, version, slot, err
	}
	return item, job, version, slot, nil
}

func (executor *TextExecutor) captureProviderResponse(ctx context.Context, executionID uint, response TextResponse) error {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return executor.db.WithContext(cleanupCtx).Transaction(func(tx *gorm.DB) error {
		query := tx
		if tx.Dialector.Name() == "mysql" {
			query = query.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		var execution models.AIExecution
		if err := query.First(&execution, executionID).Error; err != nil {
			return err
		}
		if execution.Status == models.AIExecutionStoring || execution.Status == models.AIExecutionCompleted {
			if execution.OpenAIResponseID == response.ResponseID && string(execution.ProviderOutputJSON) == string(response.OutputJSON) {
				return nil
			}
			return ErrExecutionNeedsAttention
		}
		if (execution.Status != models.AIExecutionCallingOpenAI && execution.Status != models.AIExecutionNeedsAttention) || len(execution.ProviderOutputJSON) != 0 || execution.OpenAIResponseID != "" {
			return ErrExecutionNeedsAttention
		}
		result := tx.Model(&execution).Where("status IN ?", []models.AIExecutionStatus{models.AIExecutionCallingOpenAI, models.AIExecutionNeedsAttention}).Updates(map[string]any{
			"status": models.AIExecutionStoring, "provider_output_json": []byte(response.OutputJSON), "open_ai_response_id": response.ResponseID,
			"open_ai_request_id": response.RequestID, "model": response.Model, "input_text_tokens": response.Usage.InputTextTokens, "output_text_tokens": response.Usage.OutputTextTokens,
			"reasoning_tokens": response.Usage.ReasoningTokens, "total_tokens": response.Usage.TotalTokens,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrExecutionNeedsAttention
		}
		return nil
	})
}

func (executor *TextExecutor) finalize(ctx context.Context, execution models.AIExecution, output json.RawMessage) error {
	var envelope struct {
		Candidates []json.RawMessage `json:"candidates"`
	}
	if err := decodeStrictJSON(output, &envelope); err != nil || len(envelope.Candidates) == 0 {
		return ErrTextProviderInvalidResponse
	}
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return executor.db.WithContext(cleanupCtx).Transaction(func(tx *gorm.DB) error {
		query := tx
		if tx.Dialector.Name() == "mysql" {
			query = query.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		var current models.AIExecution
		if err := query.First(&current, execution.ID).Error; err != nil {
			return err
		}
		if current.Status == models.AIExecutionCompleted {
			return nil
		}
		if current.Status != models.AIExecutionStoring || len(current.ProviderOutputJSON) == 0 {
			return ErrExecutionNeedsAttention
		}
		var item models.AIJobItem
		if err := tx.First(&item, current.AIJobItemID).Error; err != nil {
			return err
		}
		for index, candidate := range envelope.Candidates {
			result := models.AITextResult{PublicID: uuid.NewString(), AIExecutionID: current.ID, CandidateIndex: index + 1, Kind: item.Kind, RawStructuredJSON: candidate, ValidationJSON: []byte(`[]`), State: models.AITextResultCandidate}
			if err := tx.Create(&result).Error; err != nil {
				return err
			}
		}
		ledger := models.AIUsageLedger{AIExecutionID: current.ID, Model: current.Model, InputTextTokens: current.InputTextTokens, OutputTextTokens: current.OutputTextTokens, ReasoningTokens: current.ReasoningTokens, TotalTokens: current.TotalTokens, Currency: "USD", OpenAIRequestID: current.OpenAIRequestID}
		if err := tx.Create(&ledger).Error; err != nil {
			return err
		}
		now := executor.clock.Now()
		metadata, err := json.Marshal(map[string]any{"model": current.Model, "candidate_count": len(envelope.Candidates), "openai_request_id": current.OpenAIRequestID, "openai_response_id": current.OpenAIResponseID, "usage": map[string]int64{"input_text_tokens": current.InputTextTokens, "output_text_tokens": current.OutputTextTokens, "reasoning_tokens": current.ReasoningTokens, "total_tokens": current.TotalTokens}})
		if err != nil {
			return err
		}
		jobID, itemID, executionID := item.AIJobID, item.ID, current.ID
		audit := models.AIAuditEvent{PublicID: uuid.NewString(), EventType: "ai_execution.text_completed", EntityType: "ai_execution", EntityPublicID: current.PublicID, AIJobID: &jobID, AIJobItemID: &itemID, AIExecutionID: &executionID, MetadataJSON: metadata}
		if err := tx.Create(&audit).Error; err != nil {
			return err
		}
		if err := tx.Model(&current).Updates(map[string]any{"status": models.AIExecutionCompleted, "completed_at": now, "safe_error": ""}).Error; err != nil {
			return err
		}
		promoted := tx.Model(&models.AIJobItem{}).Where("id = ? AND status = ?", item.ID, models.AIJobItemFailed).Updates(map[string]any{
			"status": models.AIJobItemCompleted, "safe_error": "", "internal_error": "", "lease_owner": "", "lease_expires_at": nil, "completed_at": now,
		})
		if promoted.Error != nil {
			return promoted.Error
		}
		if promoted.RowsAffected == 1 {
			return aggregateJob(tx, item.AIJobID, now)
		}
		return nil
	})
}

func validExecutorTextResponse(response TextResponse, prompt CompiledTextPrompt) bool {
	usage := response.Usage
	return response.ResponseID != "" && response.Model != "" && usage.InputTextTokens >= 0 && usage.OutputTextTokens >= 0 && usage.TotalTokens >= 0 && usage.ReasoningTokens >= 0 && usage.ReasoningTokens <= usage.OutputTextTokens &&
		usage.TotalTokens == usage.InputTextTokens+usage.OutputTextTokens && validateTextCandidates(response.OutputJSON, prompt) == nil
}

func (executor *TextExecutor) markProviderFailure(ctx context.Context, executionID uint, status models.AIExecutionStatus, safeError, requestID string) error {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return executor.db.WithContext(cleanupCtx).Transaction(func(tx *gorm.DB) error {
		var execution models.AIExecution
		query := tx
		if tx.Dialector.Name() == "mysql" {
			query = query.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		if err := query.First(&execution, executionID).Error; err != nil {
			return err
		}
		if execution.Status != models.AIExecutionPreparing && execution.Status != models.AIExecutionCallingOpenAI {
			if (execution.Status == models.AIExecutionFailed || execution.Status == models.AIExecutionNeedsAttention) && requestID != "" && execution.OpenAIRequestID == "" {
				if err := tx.Model(&execution).Update("open_ai_request_id", requestID).Error; err != nil {
					return err
				}
				execution.OpenAIRequestID = requestID
				return executor.upsertTerminalAudit(tx, execution, execution.Status, execution.SafeError, requestID)
			}
			return nil
		}
		now := executor.clock.Now()
		updates := map[string]any{"status": status, "safe_error": safeError, "completed_at": now}
		if requestID != "" || execution.OpenAIRequestID == "" {
			updates["open_ai_request_id"] = requestID
		}
		if err := tx.Model(&execution).Where("status IN ?", []models.AIExecutionStatus{models.AIExecutionPreparing, models.AIExecutionCallingOpenAI}).Updates(updates).Error; err != nil {
			return err
		}
		execution.Status = status
		execution.SafeError = safeError
		if requestID != "" {
			execution.OpenAIRequestID = requestID
		}
		return executor.upsertTerminalAudit(tx, execution, status, safeError, execution.OpenAIRequestID)
	})
}

func (executor *TextExecutor) upsertTerminalAudit(tx *gorm.DB, execution models.AIExecution, status models.AIExecutionStatus, safeError, requestID string) error {
	var item models.AIJobItem
	if err := tx.Select("id", "ai_job_id").First(&item, execution.AIJobItemID).Error; err != nil {
		return err
	}
	eventType := "ai_execution.text_failed"
	if status == models.AIExecutionNeedsAttention {
		eventType = "ai_execution.text_needs_attention"
	}
	metadata, err := json.Marshal(map[string]any{"status": status, "safe_error": safeError, "openai_request_id": requestID, "key_fingerprint": execution.OpenAIKeyFingerprint})
	if err != nil {
		return err
	}
	jobID, itemID := item.AIJobID, item.ID
	idempotencyKey := execution.PublicID + ":" + eventType
	audit := models.AIAuditEvent{PublicID: uuid.NewString(), EventType: eventType, EntityType: "ai_execution", EntityPublicID: execution.PublicID, IdempotencyKey: &idempotencyKey, AIJobID: &jobID, AIJobItemID: &itemID, AIExecutionID: &execution.ID, MetadataJSON: metadata}
	var existing models.AIAuditEvent
	err = tx.Where("ai_execution_id = ? AND event_type = ?", execution.ID, eventType).First(&existing).Error
	if err == nil {
		return tx.Model(&existing).Update("metadata_json", metadata).Error
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	return tx.Create(&audit).Error
}

func providerFailureState(err error) (models.AIExecutionStatus, string) {
	switch {
	case errors.Is(err, ErrTextProviderAmbiguousTimeout), errors.Is(err, ErrTextProviderAmbiguousTransport):
		return models.AIExecutionNeedsAttention, "OpenAI call outcome is ambiguous"
	case errors.Is(err, ErrTextProviderAuthentication):
		return models.AIExecutionFailed, "OpenAI authentication failed"
	case errors.Is(err, ErrTextProviderRefusal):
		return models.AIExecutionFailed, "OpenAI refused the content request"
	default:
		return models.AIExecutionFailed, "OpenAI text generation failed"
	}
}

func providerRequestID(err error) string {
	var providerErr *TextProviderError
	if errors.As(err, &providerErr) {
		return providerErr.RequestID
	}
	return ""
}

func clearBytes(value []byte) {
	for index := range value {
		value[index] = 0
	}
}

type KindRoutingExecutor struct {
	dryRun     bool
	dryRunExec ItemExecutor
	textExec   ItemExecutor
}

func NewKindRoutingExecutor(dryRun bool, dryRunExecutor, textExecutor ItemExecutor) *KindRoutingExecutor {
	return &KindRoutingExecutor{dryRun: dryRun, dryRunExec: dryRunExecutor, textExec: textExecutor}
}

func (executor *KindRoutingExecutor) Execute(ctx context.Context, item LeasedItem) error {
	if executor.dryRun {
		if executor.dryRunExec == nil {
			return fmt.Errorf("dry-run executor is unavailable")
		}
		return executor.dryRunExec.Execute(ctx, item)
	}
	switch item.Kind {
	case models.AIContentSlotTitle, models.AIContentSlotSEODescription:
		if executor.textExec == nil {
			return fmt.Errorf("text executor is unavailable")
		}
		return executor.textExec.Execute(ctx, item)
	case models.AIContentSlotImage:
		return ErrRealImageGenerationUnsupported
	default:
		return ErrExecutionInputInvalid
	}
}
