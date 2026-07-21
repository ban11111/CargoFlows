package app

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"cargoflows/api/internal/ai"
	"cargoflows/api/internal/models"
	"cargoflows/api/internal/money"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type aiCostSettingRequest struct {
	AdminAPIKey string `json:"admin_api_key"`
	ProjectID   string `json:"project_id"`
	APIKeyID    string `json:"api_key_id"`
}
type aiCostScopesRequest struct {
	AdminAPIKey string `json:"admin_api_key"`
	ProjectID   string `json:"project_id"`
}
type aiRateVersionRequest struct {
	EffectiveAt string `json:"effective_at"`
	Rates       []struct {
		Model       string `json:"model"`
		APIMode     string `json:"api_mode"`
		ServiceTier string `json:"service_tier"`
		Metric      string `json:"metric"`
		UnitRateUSD string `json:"unit_rate_usd"`
	} `json:"rates"`
}
type dateRangeRequest struct {
	StartDate string `json:"start_date"`
	EndDate   string `json:"end_date"`
}
type closePeriodRequest struct {
	InvoiceReference string `json:"invoice_reference"`
}

func (s *Server) getOpenAICostSetting(c *gin.Context) {
	if s.ai.Costs == nil {
		respondAIUnavailable(c)
		return
	}
	value, err := s.ai.Costs.Get(c.Request.Context())
	if err != nil {
		respondAIError(c, err)
		return
	}
	c.JSON(http.StatusOK, value)
}
func (s *Server) putOpenAICostSetting(c *gin.Context) {
	if s.ai.Costs == nil {
		respondAIUnavailable(c)
		return
	}
	var req aiCostSettingRequest
	if err := decodeJSONStrict(c, &req); err != nil {
		respondAIBadRequest(c, err)
		return
	}
	value, err := s.ai.Costs.Configure(c.Request.Context(), currentUser(c).ID, req.AdminAPIKey, req.ProjectID, req.APIKeyID)
	if err != nil {
		respondAIError(c, err)
		return
	}
	c.JSON(http.StatusOK, value)
}
func (s *Server) listOpenAICostScopes(c *gin.Context) {
	if s.ai.Costs == nil {
		respondAIUnavailable(c)
		return
	}
	var req aiCostScopesRequest
	if err := decodeJSONStrict(c, &req); err != nil || strings.TrimSpace(req.AdminAPIKey) == "" {
		respondAIBadRequest(c, errors.New("admin_api_key is required"))
		return
	}
	value, err := s.ai.Costs.ListScopes(c.Request.Context(), req.AdminAPIKey, req.ProjectID)
	if err != nil {
		respondAIError(c, err)
		return
	}
	c.JSON(http.StatusOK, value)
}

func (s *Server) listAICostRates(c *gin.Context) {
	var rows []models.AIRateCard
	if err := s.db.Order("version DESC, model, api_mode, service_tier, metric").Find(&rows).Error; err != nil {
		respondAIError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": rows})
}
func (s *Server) createAICostRateVersion(c *gin.Context) {
	var req aiRateVersionRequest
	if err := decodeJSONStrict(c, &req); err != nil {
		respondAIBadRequest(c, err)
		return
	}
	effective, err := time.Parse(time.RFC3339, req.EffectiveAt)
	if err != nil || len(req.Rates) == 0 {
		respondAIBadRequest(c, errors.New("effective_at and at least one rate are required"))
		return
	}
	metrics := map[string]bool{"input_text": true, "cached_input": true, "input_image": true, "output_text": true, "output_image": true}
	var version int
	if err := s.db.Model(&models.AIRateCard{}).Select("COALESCE(MAX(version),0)").Scan(&version).Error; err != nil {
		respondAIError(c, err)
		return
	}
	version++
	rows := make([]models.AIRateCard, 0, len(req.Rates))
	seen := map[string]bool{}
	for _, input := range req.Rates {
		input.Model = strings.TrimSpace(input.Model)
		input.APIMode = strings.TrimSpace(input.APIMode)
		input.ServiceTier = strings.TrimSpace(input.ServiceTier)
		input.Metric = strings.TrimSpace(input.Metric)
		if input.ServiceTier == "" {
			input.ServiceTier = "default"
		}
		rate, parseErr := money.Parse(input.UnitRateUSD)
		key := input.Model + "\x00" + input.APIMode + "\x00" + input.ServiceTier + "\x00" + input.Metric
		if parseErr != nil || rate.Sign() < 0 || input.Model == "" || (input.APIMode != "responses" && input.APIMode != "images") || !metrics[input.Metric] || seen[key] {
			respondAIBadRequest(c, errors.New("invalid or duplicate rate entry"))
			return
		}
		seen[key] = true
		rows = append(rows, models.AIRateCard{Version: version, Model: input.Model, APIMode: input.APIMode, ServiceTier: input.ServiceTier, Metric: input.Metric, UnitSize: 1000000, UnitRateUSD: money.Format(rate), EffectiveAt: effective.UTC(), CreatedByID: currentUser(c).ID})
	}
	if err := s.db.Create(&rows).Error; err != nil {
		respondAIError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"version": version, "data": rows})
}

func parseDateRange(startValue, endValue string) (time.Time, time.Time, error) {
	start, err := time.Parse("2006-01-02", startValue)
	if err != nil {
		return start, time.Time{}, errors.New("start_date must be YYYY-MM-DD")
	}
	end, err := time.Parse("2006-01-02", endValue)
	if err != nil {
		return start, end, errors.New("end_date must be YYYY-MM-DD")
	}
	end = end.AddDate(0, 0, 1)
	if !end.After(start) {
		return start, end, errors.New("end_date must not precede start_date")
	}
	return start, end, nil
}
func (s *Server) repriceAIUsage(c *gin.Context) {
	var req dateRangeRequest
	if err := decodeJSONStrict(c, &req); err != nil {
		respondAIBadRequest(c, err)
		return
	}
	start, end, err := parseDateRange(req.StartDate, req.EndDate)
	if err != nil {
		respondAIBadRequest(c, err)
		return
	}
	var ledgers []models.AIUsageLedger
	if err := s.db.Where("created_at >= ? AND created_at < ? AND pricing_status <> ?", start, end, "priced").Find(&ledgers).Error; err != nil {
		respondAIError(c, err)
		return
	}
	count := 0
	for i := range ledgers {
		ledger := &ledgers[i]
		var execution models.AIExecution
		if s.db.First(&execution, ledger.AIExecutionID).Error != nil {
			continue
		}
		err := s.db.Transaction(func(tx *gorm.DB) error {
			return ai.PriceUsageLedger(tx, ledger, execution)
		})
		if err != nil {
			respondAIError(c, err)
			return
		}
		count++
	}
	c.JSON(http.StatusOK, gin.H{"repriced": count})
}
func (s *Server) syncAIReconciliation(c *gin.Context) {
	if s.ai.Costs == nil {
		respondAIUnavailable(c)
		return
	}
	var req dateRangeRequest
	if err := decodeJSONStrict(c, &req); err != nil {
		respondAIBadRequest(c, err)
		return
	}
	start, end, err := parseDateRange(req.StartDate, req.EndDate)
	if err != nil {
		respondAIBadRequest(c, err)
		return
	}
	count, err := s.ai.Costs.Sync(c.Request.Context(), start, end)
	if err != nil {
		respondAIError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"synced_buckets": count})
}
func (s *Server) listAIReconciliation(c *gin.Context) {
	if s.ai.Costs == nil {
		respondAIUnavailable(c)
		return
	}
	start, end, err := parseDateRange(c.Query("start_date"), c.Query("end_date"))
	if err != nil {
		respondAIBadRequest(c, err)
		return
	}
	rows, err := s.ai.Costs.Summary(c.Request.Context(), start, end)
	if err != nil {
		respondAIError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": rows})
}

func (s *Server) listAICostAnalytics(c *gin.Context) {
	start, end, err := parseDateRange(c.Query("start_date"), c.Query("end_date"))
	if err != nil {
		respondAIBadRequest(c, err)
		return
	}
	dimensions := map[string]string{
		"date":     "DATE(ledger.created_at)",
		"sku":      "sku.code",
		"user":     "COALESCE(actor.name, '')",
		"model":    "COALESCE(NULLIF(execution.actual_model, ''), execution.model)",
		"template": "template_version.public_id",
		"platform": "job.target_platform",
	}
	group := strings.TrimSpace(c.DefaultQuery("group_by", "date"))
	expression, ok := dimensions[group]
	if !ok {
		respondAIBadRequest(c, errors.New("group_by must be date, sku, user, model, template, or platform"))
		return
	}
	var rows []struct {
		DimensionValue     string `json:"dimension_value"`
		TotalTokens        int64  `json:"total_tokens"`
		EstimatedAmountUSD string `json:"estimated_amount_usd"`
		UnpricedCount      int64  `json:"unpriced_count"`
	}
	err = s.db.Table("ai_usage_ledgers AS ledger").
		Select(expression+" AS dimension_value, COALESCE(SUM(ledger.total_tokens),0) AS total_tokens, COALESCE(SUM(CASE WHEN ledger.pricing_status = 'priced' THEN ledger.estimated_amount_usd ELSE 0 END),0) AS estimated_amount_usd, SUM(CASE WHEN ledger.pricing_status IN ('unpriced','partial') THEN 1 ELSE 0 END) AS unpriced_count").
		Joins("JOIN ai_executions AS execution ON execution.id = ledger.ai_execution_id").
		Joins("JOIN ai_job_items AS item ON item.id = execution.ai_job_item_id").
		Joins("JOIN ai_jobs AS job ON job.id = item.ai_job_id").
		Joins("JOIN skus AS sku ON sku.id = job.sk_uid").
		Joins("JOIN ai_content_template_versions AS template_version ON template_version.id = job.ai_content_template_version_id").
		Joins("LEFT JOIN users AS actor ON actor.id = job.created_by_id").
		Where("ledger.created_at >= ? AND ledger.created_at < ?", start, end).
		Group(expression).Order("estimated_amount_usd DESC").Scan(&rows).Error
	if err != nil {
		respondAIError(c, err)
		return
	}
	for index := range rows {
		rows[index].EstimatedAmountUSD = decimalOrZero(rows[index].EstimatedAmountUSD)
	}
	c.JSON(http.StatusOK, gin.H{"group_by": group, "data": rows})
}
func (s *Server) closeAIReconciliationPeriod(c *gin.Context) {
	if s.ai.Costs == nil {
		respondAIUnavailable(c)
		return
	}
	var req closePeriodRequest
	if err := decodeJSONStrict(c, &req); err != nil {
		respondAIBadRequest(c, err)
		return
	}
	row, err := s.ai.Costs.ClosePeriod(c.Request.Context(), c.Param("month"), req.InvoiceReference, currentUser(c).ID, true)
	if err != nil {
		respondAIError(c, err)
		return
	}
	c.JSON(http.StatusOK, row)
}
func (s *Server) reopenAIReconciliationPeriod(c *gin.Context) {
	if s.ai.Costs == nil {
		respondAIUnavailable(c)
		return
	}
	row, err := s.ai.Costs.ClosePeriod(c.Request.Context(), c.Param("month"), "", currentUser(c).ID, false)
	if err != nil {
		respondAIError(c, err)
		return
	}
	c.JSON(http.StatusOK, row)
}
