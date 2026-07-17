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
	ErrTextResultNotFound          = errors.New("AI text result not found")
	ErrTextResultInvalid           = errors.New("AI text result is invalid")
	ErrTextResultLifecycleConflict = errors.New("AI text result lifecycle conflict")
	ErrTextResultApprovalRequired  = errors.New("AI text result approval is required")
	ErrTextResultNotEffective      = errors.New("AI text result is not the effective approval")
)

type TextResultDocument struct {
	PublicID         string                   `json:"public_id"`
	JobItemPublicID  string                   `json:"job_item_id"`
	CandidateIndex   int                      `json:"candidate_index"`
	Kind             models.AIContentSlotKind `json:"kind"`
	RawStructured    json.RawMessage          `json:"raw_structured"`
	EditedStructured json.RawMessage          `json:"edited_structured,omitempty"`
	Validation       json.RawMessage          `json:"validation"`
	State            models.AITextResultState `json:"state"`
	EditedAt         *time.Time               `json:"edited_at"`
	ApprovedAt       *time.Time               `json:"approved_at"`
	RejectedAt       *time.Time               `json:"rejected_at"`
	AppliedAt        *time.Time               `json:"applied_at"`
	Effective        bool                     `json:"effective"`
	CreatedAt        time.Time                `json:"created_at"`
	UpdatedAt        time.Time                `json:"updated_at"`
}

type PlatformContentDocument struct {
	PublicID         string          `json:"public_id"`
	SKUID            string          `json:"sku_id"`
	Platform         string          `json:"platform"`
	Locale           string          `json:"locale"`
	Title            string          `json:"title"`
	ShortDescription string          `json:"short_description"`
	LongDescription  string          `json:"long_description"`
	SellingPoints    json.RawMessage `json:"selling_points"`
	SearchKeywords   json.RawMessage `json:"search_keywords"`
	Revision         int             `json:"revision"`
	UpdatedAt        time.Time       `json:"updated_at"`
}

type PlatformContentRevisionDocument struct {
	PublicID  string          `json:"public_id"`
	Revision  int             `json:"revision"`
	Before    json.RawMessage `json:"before"`
	After     json.RawMessage `json:"after"`
	CreatedAt time.Time       `json:"created_at"`
}

type PlatformContentHistory struct {
	Content   *PlatformContentDocument          `json:"content"`
	Revisions []PlatformContentRevisionDocument `json:"revisions"`
}

type TextApplicationPreview struct {
	Before json.RawMessage `json:"before"`
	After  json.RawMessage `json:"after"`
}

type TextApplicationResult struct {
	Content  PlatformContentDocument `json:"content"`
	Replayed bool                    `json:"replayed"`
}

type TextResultService struct {
	db    *gorm.DB
	clock Clock
}

func NewTextResultService(db *gorm.DB) *TextResultService {
	return &TextResultService{db: db, clock: SystemClock{}}
}

func (service *TextResultService) List(ctx context.Context, jobPublicID string) ([]TextResultDocument, error) {
	var job models.AIJob
	if err := service.db.WithContext(ctx).Where("public_id = ?", jobPublicID).First(&job).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrTextResultNotFound
		}
		return nil, err
	}
	var items []models.AIJobItem
	if err := service.db.WithContext(ctx).Where("ai_job_id = ?", job.ID).Find(&items).Error; err != nil {
		return nil, err
	}
	documents := make([]TextResultDocument, 0)
	for _, item := range items {
		var executions []models.AIExecution
		if err := service.db.WithContext(ctx).Select("id").Where("ai_job_item_id = ?", item.ID).Find(&executions).Error; err != nil {
			return nil, err
		}
		for _, execution := range executions {
			var results []models.AITextResult
			if err := service.db.WithContext(ctx).Where("ai_execution_id = ?", execution.ID).Order("candidate_index ASC").Find(&results).Error; err != nil {
				return nil, err
			}
			for _, result := range results {
				documents = append(documents, textResultDocument(result, item))
			}
		}
	}
	return documents, nil
}

func (service *TextResultService) Edit(ctx context.Context, jobID, itemID, resultID string, actorID uint, structured json.RawMessage) (TextResultDocument, error) {
	var document TextResultDocument
	err := service.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		binding, err := loadTextResultBinding(tx, jobID, itemID, resultID, true)
		if err != nil {
			return err
		}
		if binding.result.State == models.AITextResultRejected || binding.result.AppliedAt != nil {
			return ErrTextResultLifecycleConflict
		}
		if err := validateEditedTextResult(binding.job, binding.item, binding.result.Kind, structured); err != nil {
			return err
		}
		before := effectiveTextJSON(binding.result)
		now := service.clock.Now()
		approvalInvalidated := binding.result.State == models.AITextResultApproved
		updates := map[string]any{"edited_structured_json": []byte(structured), "edited_by_id": actorID, "edited_at": now}
		if approvalInvalidated {
			updates["state"] = models.AITextResultCandidate
			updates["approved_by_id"] = nil
			updates["approved_at"] = nil
		}
		if err := tx.Model(&binding.result).Updates(updates).Error; err != nil {
			return err
		}
		if approvalInvalidated && binding.item.EffectiveApprovedResultID != nil && *binding.item.EffectiveApprovedResultID == binding.result.ID {
			if err := tx.Model(&binding.item).Update("effective_approved_result_id", nil).Error; err != nil {
				return err
			}
			binding.item.EffectiveApprovedResultID = nil
		}
		if err := createTextResultAudit(tx, binding, actorID, "ai_text_result.edited", map[string]any{"before": before, "after": json.RawMessage(structured), "approval_invalidated": approvalInvalidated}); err != nil {
			return err
		}
		if err := tx.First(&binding.result, binding.result.ID).Error; err != nil {
			return err
		}
		document = textResultDocument(binding.result, binding.item)
		return nil
	})
	return document, err
}

func (service *TextResultService) Approve(ctx context.Context, jobID, itemID, resultID string, actorID uint) (TextResultDocument, error) {
	var document TextResultDocument
	err := service.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		binding, err := loadTextResultBinding(tx, jobID, itemID, resultID, true)
		if err != nil {
			return err
		}
		if binding.result.State == models.AITextResultRejected || binding.result.AppliedAt != nil {
			return ErrTextResultLifecycleConflict
		}
		if err := validateEditedTextResult(binding.job, binding.item, binding.result.Kind, effectiveTextJSON(binding.result)); err != nil {
			return err
		}
		now := service.clock.Now()
		if binding.result.State != models.AITextResultApproved {
			if err := tx.Model(&binding.result).Updates(map[string]any{"state": models.AITextResultApproved, "approved_by_id": actorID, "approved_at": now}).Error; err != nil {
				return err
			}
		}
		if err := tx.Model(&binding.item).Update("effective_approved_result_id", binding.result.ID).Error; err != nil {
			return err
		}
		binding.item.EffectiveApprovedResultID = &binding.result.ID
		if err := createTextResultAudit(tx, binding, actorID, "ai_text_result.approved", map[string]any{"effective": true}); err != nil {
			return err
		}
		if err := tx.First(&binding.result, binding.result.ID).Error; err != nil {
			return err
		}
		document = textResultDocument(binding.result, binding.item)
		return nil
	})
	return document, err
}

func (service *TextResultService) Reject(ctx context.Context, jobID, itemID, resultID string, actorID uint) (TextResultDocument, error) {
	var document TextResultDocument
	err := service.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		binding, err := loadTextResultBinding(tx, jobID, itemID, resultID, true)
		if err != nil {
			return err
		}
		if binding.result.AppliedAt != nil {
			return ErrTextResultLifecycleConflict
		}
		if binding.result.State != models.AITextResultRejected {
			now := service.clock.Now()
			updates := map[string]any{"state": models.AITextResultRejected, "rejected_by_id": actorID, "rejected_at": now, "approved_by_id": nil, "approved_at": nil}
			if err := tx.Model(&binding.result).Updates(updates).Error; err != nil {
				return err
			}
		}
		if binding.item.EffectiveApprovedResultID != nil && *binding.item.EffectiveApprovedResultID == binding.result.ID {
			if err := tx.Model(&binding.item).Update("effective_approved_result_id", nil).Error; err != nil {
				return err
			}
			binding.item.EffectiveApprovedResultID = nil
		}
		if err := createTextResultAudit(tx, binding, actorID, "ai_text_result.rejected", map[string]any{}); err != nil {
			return err
		}
		if err := tx.First(&binding.result, binding.result.ID).Error; err != nil {
			return err
		}
		document = textResultDocument(binding.result, binding.item)
		return nil
	})
	return document, err
}

func (service *TextResultService) Preview(ctx context.Context, jobID, itemID, resultID string) (TextApplicationPreview, error) {
	binding, err := loadTextResultBinding(service.db.WithContext(ctx), jobID, itemID, resultID, false)
	if err != nil {
		return TextApplicationPreview{}, err
	}
	if err := requireEffectiveApproval(binding); err != nil {
		return TextApplicationPreview{}, err
	}
	content, found, err := findPlatformContent(service.db.WithContext(ctx), binding.job)
	if err != nil {
		return TextApplicationPreview{}, err
	}
	before, after, err := applicationSnapshots(content, found, binding)
	return TextApplicationPreview{Before: before, After: after}, err
}

func (service *TextResultService) Apply(ctx context.Context, jobID, itemID, resultID string, actorID uint) (TextApplicationResult, error) {
	var applied TextApplicationResult
	err := service.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		binding, err := loadTextResultBinding(tx, jobID, itemID, resultID, true)
		if err != nil {
			return err
		}
		if binding.result.AppliedAt != nil {
			var revision models.SKUPlatformContentRevision
			if err := tx.Where("source_ai_text_result_id = ?", binding.result.ID).First(&revision).Error; err != nil {
				return ErrTextResultLifecycleConflict
			}
			var historical PlatformContentDocument
			if err := json.Unmarshal(revision.AfterJSON, &historical); err != nil {
				return ErrTextResultLifecycleConflict
			}
			applied = TextApplicationResult{Content: historical, Replayed: true}
			return nil
		}
		if err := requireEffectiveApproval(binding); err != nil {
			return err
		}
		skuQuery := tx
		if tx.Dialector.Name() == "mysql" {
			skuQuery = skuQuery.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		var sku models.SKU
		if err := skuQuery.Select("id").First(&sku, binding.job.SKUID).Error; err != nil {
			return err
		}
		content, found, err := findPlatformContent(tx, binding.job)
		if err != nil {
			return err
		}
		before, after, err := applicationSnapshots(content, found, binding)
		if err != nil {
			return err
		}
		var next PlatformContentDocument
		if err := json.Unmarshal(after, &next); err != nil {
			return err
		}
		now := service.clock.Now()
		if !found {
			content = models.SKUPlatformContent{PublicID: uuid.NewString(), SKUID: binding.job.SKUID, Platform: binding.job.TargetPlatform, Locale: binding.job.Locale, Revision: 1, UpdatedByID: actorID}
		} else {
			content.Revision++
			content.UpdatedByID = actorID
		}
		content.Title, content.ShortDescription, content.LongDescription = next.Title, next.ShortDescription, next.LongDescription
		content.SellingPointsJSON, content.SearchKeywordsJSON = []byte(next.SellingPoints), []byte(next.SearchKeywords)
		content.SourceAITextResultID = &binding.result.ID
		if !found {
			if err := tx.Create(&content).Error; err != nil {
				return err
			}
		} else if err := tx.Save(&content).Error; err != nil {
			return err
		}
		after, err = json.Marshal(platformContentDocument(content, binding.job.SKU.PublicID))
		if err != nil {
			return err
		}
		revision := models.SKUPlatformContentRevision{PublicID: uuid.NewString(), SKUPlatformContentID: content.ID, Revision: content.Revision, BeforeJSON: before, AfterJSON: after, SourceAITextResultID: &binding.result.ID, ActorID: actorID}
		if err := tx.Create(&revision).Error; err != nil {
			return err
		}
		if err := tx.Model(&binding.result).Updates(map[string]any{"applied_by_id": actorID, "applied_at": now}).Error; err != nil {
			return err
		}
		if err := createTextResultAudit(tx, binding, actorID, "ai_text_result.applied", map[string]any{"platform": binding.job.TargetPlatform, "locale": binding.job.Locale, "revision": content.Revision}); err != nil {
			return err
		}
		applied = TextApplicationResult{Content: platformContentDocument(content, binding.job.SKU.PublicID)}
		return nil
	})
	return applied, err
}

func (service *TextResultService) GetPlatformContent(ctx context.Context, skuPublicID string, platform, locale string) (PlatformContentHistory, error) {
	history := PlatformContentHistory{Revisions: []PlatformContentRevisionDocument{}}
	err := service.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		parsed, err := uuid.Parse(skuPublicID)
		if err != nil || parsed == uuid.Nil {
			return ErrSKUNotFound
		}
		var sku models.SKU
		if err := tx.Select("id", "public_id").Where("public_id = ?", parsed.String()).First(&sku).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrSKUNotFound
			}
			return err
		}
		var content models.SKUPlatformContent
		err = tx.Where("sku_id = ? AND platform = ? AND locale = ?", sku.ID, platform, locale).First(&content).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		var revisions []models.SKUPlatformContentRevision
		if err := tx.Where("sku_platform_content_id = ?", content.ID).Order("revision DESC").Find(&revisions).Error; err != nil {
			return err
		}
		doc := platformContentDocument(content, sku.PublicID)
		history.Content = &doc
		history.Revisions = make([]PlatformContentRevisionDocument, 0, len(revisions))
		for _, revision := range revisions {
			history.Revisions = append(history.Revisions, PlatformContentRevisionDocument{PublicID: revision.PublicID, Revision: revision.Revision, Before: cloneRawJSON(revision.BeforeJSON, `{}`), After: cloneRawJSON(revision.AfterJSON, `{}`), CreatedAt: revision.CreatedAt})
		}
		return nil
	})
	return history, err
}

type textResultBinding struct {
	job       models.AIJob
	item      models.AIJobItem
	execution models.AIExecution
	result    models.AITextResult
}

func loadTextResultBinding(db *gorm.DB, jobPublicID, itemPublicID, resultPublicID string, lock bool) (textResultBinding, error) {
	newQuery := func() *gorm.DB {
		query := db.Session(&gorm.Session{NewDB: true})
		if lock && db.Dialector.Name() == "mysql" {
			query = query.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		return query
	}
	var binding textResultBinding
	if err := newQuery().Preload("SKU").Where("public_id = ?", jobPublicID).First(&binding.job).Error; err != nil {
		return binding, textResultNotFound(err)
	}
	if err := newQuery().Where("public_id = ? AND ai_job_id = ?", itemPublicID, binding.job.ID).First(&binding.item).Error; err != nil {
		return binding, textResultNotFound(err)
	}
	if err := newQuery().Where("public_id = ?", resultPublicID).First(&binding.result).Error; err != nil {
		return binding, textResultNotFound(err)
	}
	if err := newQuery().Where("id = ? AND ai_job_item_id = ?", binding.result.AIExecutionID, binding.item.ID).First(&binding.execution).Error; err != nil {
		return binding, ErrTextResultNotFound
	}
	return binding, nil
}

func textResultNotFound(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrTextResultNotFound
	}
	return err
}

func validateEditedTextResult(job models.AIJob, item models.AIJobItem, kind models.AIContentSlotKind, raw json.RawMessage) error {
	var slot SlotFacts
	if decodeStrictJSON(item.SlotSnapshotJSON, &slot) != nil || slot.Kind != kind {
		return ErrTextResultInvalid
	}
	var snapshot ProductSnapshotV1
	if decodeStrictJSON(job.InputSnapshotJSON, &snapshot) != nil {
		return ErrTextResultInvalid
	}
	constraints, err := parseTextConstraintRules(slot.Constraints, kind)
	if err != nil {
		return ErrTextResultInvalid
	}
	bounds := textLengthBounds{MinLength: constraints.MinLength, MaxLength: constraints.MaxLength}
	switch kind {
	case models.AIContentSlotTitle:
		var candidate titleTextCandidate
		if strictJSONDecode(raw, &candidate) != nil || candidate.Title == nil || candidate.Keywords == nil || candidate.SourceFields == nil || !withinTextBounds(*candidate.Title, bounds) || !validateTitleCandidateRules(candidate, constraints, requiredTextFieldValues(snapshot.Product, snapshot.SKU)) {
			return ErrTextResultInvalid
		}
	case models.AIContentSlotSEODescription:
		var candidate seoTextCandidate
		if strictJSONDecode(raw, &candidate) != nil || candidate.ShortDescription == nil || candidate.SellingPoints == nil || candidate.LongDescription == nil || candidate.SearchKeywords == nil || candidate.SourceFields == nil || !withinTextBounds(*candidate.ShortDescription, bounds) || !withinTextBounds(*candidate.LongDescription, bounds) || !validateSEOCandidateRules(candidate, constraints, requiredTextFieldValues(snapshot.Product, snapshot.SKU)) {
			return ErrTextResultInvalid
		}
	default:
		return ErrTextResultInvalid
	}
	return nil
}

func requireEffectiveApproval(binding textResultBinding) error {
	if binding.result.State != models.AITextResultApproved {
		return ErrTextResultApprovalRequired
	}
	if binding.item.EffectiveApprovedResultID == nil || *binding.item.EffectiveApprovedResultID != binding.result.ID {
		return ErrTextResultNotEffective
	}
	return validateEditedTextResult(binding.job, binding.item, binding.result.Kind, effectiveTextJSON(binding.result))
}

func effectiveTextJSON(result models.AITextResult) json.RawMessage {
	if len(result.EditedStructuredJSON) != 0 {
		return append(json.RawMessage(nil), result.EditedStructuredJSON...)
	}
	return append(json.RawMessage(nil), result.RawStructuredJSON...)
}

func findPlatformContent(db *gorm.DB, job models.AIJob) (models.SKUPlatformContent, bool, error) {
	query := db.Where("sku_id = ? AND platform = ? AND locale = ?", job.SKUID, job.TargetPlatform, job.Locale)
	if db.Dialector.Name() == "mysql" {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var content models.SKUPlatformContent
	err := query.First(&content).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return content, false, nil
	}
	return content, err == nil, err
}

func applicationSnapshots(content models.SKUPlatformContent, found bool, binding textResultBinding) (json.RawMessage, json.RawMessage, error) {
	before := json.RawMessage(`{}`)
	document := PlatformContentDocument{SKUID: binding.job.SKU.PublicID, Platform: binding.job.TargetPlatform, Locale: binding.job.Locale, SellingPoints: json.RawMessage(`[]`), SearchKeywords: json.RawMessage(`[]`), Revision: 1}
	if found {
		document = platformContentDocument(content, binding.job.SKU.PublicID)
		encoded, err := json.Marshal(document)
		if err != nil {
			return nil, nil, err
		}
		before = encoded
		document.Revision++
	}
	raw := effectiveTextJSON(binding.result)
	if binding.result.Kind == models.AIContentSlotTitle {
		var candidate titleTextCandidate
		if strictJSONDecode(raw, &candidate) != nil || candidate.Title == nil {
			return nil, nil, ErrTextResultInvalid
		}
		document.Title = *candidate.Title
	} else {
		var candidate seoTextCandidate
		if strictJSONDecode(raw, &candidate) != nil || candidate.ShortDescription == nil || candidate.LongDescription == nil || candidate.SellingPoints == nil || candidate.SearchKeywords == nil {
			return nil, nil, ErrTextResultInvalid
		}
		document.ShortDescription, document.LongDescription = *candidate.ShortDescription, *candidate.LongDescription
		document.SellingPoints, _ = json.Marshal(*candidate.SellingPoints)
		document.SearchKeywords, _ = json.Marshal(*candidate.SearchKeywords)
	}
	after, err := json.Marshal(document)
	return before, after, err
}

func platformContentDocument(content models.SKUPlatformContent, skuPublicID string) PlatformContentDocument {
	return PlatformContentDocument{PublicID: content.PublicID, SKUID: skuPublicID, Platform: content.Platform, Locale: content.Locale, Title: content.Title, ShortDescription: content.ShortDescription, LongDescription: content.LongDescription, SellingPoints: cloneRawJSON(content.SellingPointsJSON, `[]`), SearchKeywords: cloneRawJSON(content.SearchKeywordsJSON, `[]`), Revision: content.Revision, UpdatedAt: content.UpdatedAt}
}

func textResultDocument(result models.AITextResult, item models.AIJobItem) TextResultDocument {
	effective := item.EffectiveApprovedResultID != nil && *item.EffectiveApprovedResultID == result.ID
	return TextResultDocument{PublicID: result.PublicID, JobItemPublicID: item.PublicID, CandidateIndex: result.CandidateIndex, Kind: result.Kind, RawStructured: cloneRawJSON(result.RawStructuredJSON, `{}`), EditedStructured: cloneRawJSON(result.EditedStructuredJSON, ""), Validation: cloneRawJSON(result.ValidationJSON, `[]`), State: result.State, EditedAt: result.EditedAt, ApprovedAt: result.ApprovedAt, RejectedAt: result.RejectedAt, AppliedAt: result.AppliedAt, Effective: effective, CreatedAt: result.CreatedAt, UpdatedAt: result.UpdatedAt}
}

func cloneRawJSON(value []byte, fallback string) json.RawMessage {
	if len(value) == 0 {
		if fallback == "" {
			return nil
		}
		return json.RawMessage(fallback)
	}
	return append(json.RawMessage(nil), value...)
}

func createTextResultAudit(tx *gorm.DB, binding textResultBinding, actorID uint, eventType string, details map[string]any) error {
	metadata, err := json.Marshal(details)
	if err != nil {
		return err
	}
	jobID, itemID, executionID, actor := binding.job.ID, binding.item.ID, binding.execution.ID, actorID
	audit := models.AIAuditEvent{PublicID: uuid.NewString(), EventType: eventType, EntityType: "ai_text_result", EntityPublicID: binding.result.PublicID, ActorID: &actor, AIJobID: &jobID, AIJobItemID: &itemID, AIExecutionID: &executionID, MetadataJSON: metadata}
	if err := tx.Create(&audit).Error; err != nil {
		return fmt.Errorf("audit text result mutation: %w", err)
	}
	return nil
}
