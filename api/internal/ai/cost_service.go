package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/big"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"cargoflows/api/internal/models"
	"cargoflows/api/internal/money"
	"cargoflows/api/internal/secrets"
	"gorm.io/gorm"
)

type CostSettingView struct {
	Status              string     `json:"status"`
	AdminKeyFingerprint string     `json:"admin_key_fingerprint"`
	ProjectID           string     `json:"project_id"`
	APIKeyID            string     `json:"api_key_id,omitempty"`
	Scope               string     `json:"scope"`
	LastSyncedAt        *time.Time `json:"last_synced_at"`
}
type CostScope struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	RedactedValue string `json:"redacted_value,omitempty"`
}
type CostScopes struct {
	Projects []CostScope `json:"projects"`
	APIKeys  []CostScope `json:"api_keys"`
}

const (
	CostScopeProject           = "project"
	projectScopeLegacyAPIKeyID = "__project__"
	unattributedCostAPIKeyID   = "unattributed"
)

var (
	ErrCostSettingNotConfigured = errors.New("OpenAI cost synchronization is not configured")
	ErrCostSettingInvalid       = errors.New("admin_api_key and project_id are required")
	ErrCostPeriodClosed         = errors.New("date range contains a closed reconciliation month; reopen it before syncing")
	ErrCostProviderResponse     = errors.New("OpenAI cost API returned an invalid response")
)

// CostAPIError records only safe provider metadata. Provider response bodies
// are intentionally neither stored nor returned to callers.
type CostAPIError struct {
	StatusCode int
	RequestID  string
}

func (err *CostAPIError) Error() string {
	if err.StatusCode == 0 {
		return "OpenAI cost API is unavailable"
	}
	return fmt.Sprintf("OpenAI cost API returned HTTP %d", err.StatusCode)
}

type CostService struct {
	db      *gorm.DB
	box     *secrets.AESGCM
	baseURL string
	client  *http.Client
}

func NewCostService(db *gorm.DB, box *secrets.AESGCM, baseURL string, client *http.Client) *CostService {
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	return &CostService{db: db, box: box, baseURL: strings.TrimRight(baseURL, "/"), client: client}
}

func (s *CostService) Get(ctx context.Context) (CostSettingView, error) {
	var row models.OpenAICostSetting
	err := s.db.WithContext(ctx).Where("provider = ?", "openai").First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return CostSettingView{Status: "unconfigured", Scope: CostScopeProject}, nil
	}
	if err != nil {
		return CostSettingView{}, err
	}
	view := CostSettingView{Status: row.Status, AdminKeyFingerprint: row.AdminKeyFingerprint, ProjectID: row.ProjectID, Scope: CostScopeProject, LastSyncedAt: row.LastSyncedAt}
	if row.APIKeyID != projectScopeLegacyAPIKeyID {
		view.APIKeyID = row.APIKeyID
	}
	return view, nil
}
func (s *CostService) Configure(ctx context.Context, actorID uint, adminKey, projectID, _ string) (CostSettingView, error) {
	adminKey, projectID = strings.TrimSpace(adminKey), strings.TrimSpace(projectID)
	if len(adminKey) < 20 || projectID == "" {
		return CostSettingView{}, ErrCostSettingInvalid
	}
	verification := url.Values{}
	verification.Set("start_time", strconv.FormatInt(time.Now().UTC().AddDate(0, 0, -1).Unix(), 10))
	verification.Set("limit", "1")
	verification.Add("project_ids", projectID)
	if _, err := s.get(ctx, adminKey, queryPath("/organization/costs", verification)); err != nil {
		return CostSettingView{}, err
	}
	sealed, err := s.box.Seal([]byte(adminKey))
	if err != nil {
		return CostSettingView{}, err
	}
	var row models.OpenAICostSetting
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		found := tx.Where("provider = ?", "openai").First(&row).Error
		if errors.Is(found, gorm.ErrRecordNotFound) {
			row = models.OpenAICostSetting{Provider: "openai", CreatedByID: actorID}
		} else if found != nil {
			return found
		}
		row.EncryptedAdminAPIKey, row.EncryptionNonce, row.EncryptionKeyVersion = sealed.Ciphertext, sealed.Nonce, sealed.KeyVersion
		row.AdminKeyFingerprint = fingerprint(adminKey)
		row.ProjectID, row.APIKeyID, row.Status, row.UpdatedByID = projectID, projectScopeLegacyAPIKeyID, "active", actorID
		if row.ID == 0 {
			return tx.Create(&row).Error
		}
		return tx.Save(&row).Error
	})
	if err != nil {
		return CostSettingView{}, err
	}
	return s.Get(ctx)
}

func (s *CostService) ListScopes(ctx context.Context, adminKey, projectID string) (CostScopes, error) {
	projectsBody, err := s.get(ctx, adminKey, "/organization/projects?limit=100")
	if err != nil {
		return CostScopes{}, err
	}
	var projectPage struct{ Data []struct{ ID, Name string } }
	if json.Unmarshal(projectsBody, &projectPage) != nil {
		return CostScopes{}, ErrCostProviderResponse
	}
	result := CostScopes{Projects: make([]CostScope, 0, len(projectPage.Data)), APIKeys: []CostScope{}}
	for _, p := range projectPage.Data {
		result.Projects = append(result.Projects, CostScope{ID: p.ID, Name: p.Name})
	}
	// Keep API-key discovery available during rolling deployments. New clients
	// do not require it, and Configure intentionally ignores the submitted ID.
	if projectID != "" {
		keysBody, err := s.get(ctx, adminKey, "/organization/projects/"+url.PathEscape(projectID)+"/api_keys?limit=100")
		if err != nil {
			return CostScopes{}, err
		}
		var keyPage struct {
			Data []struct {
				ID            string `json:"id"`
				Name          string `json:"name"`
				RedactedValue string `json:"redacted_value"`
			}
		}
		if err := json.Unmarshal(keysBody, &keyPage); err != nil {
			return CostScopes{}, ErrCostProviderResponse
		}
		for _, key := range keyPage.Data {
			result.APIKeys = append(result.APIKeys, CostScope{ID: key.ID, Name: key.Name, RedactedValue: key.RedactedValue})
		}
	}
	return result, nil
}

func (s *CostService) Sync(ctx context.Context, start, end time.Time) (int, error) {
	setting, key, err := s.active(ctx)
	if err != nil {
		return 0, err
	}
	defer clearByteSlice(key)
	if !end.After(start) {
		return 0, errors.New("end must be after start")
	}
	var closed int64
	if err := s.db.WithContext(ctx).Model(&models.AIReconciliationPeriod{}).Where("status = ? AND month >= ? AND month <= ?", "closed", start.Format("2006-01"), end.Add(-time.Nanosecond).Format("2006-01")).Count(&closed).Error; err != nil {
		return 0, err
	}
	if closed > 0 {
		return 0, ErrCostPeriodClosed
	}
	costParams := url.Values{}
	costParams.Set("start_time", strconv.FormatInt(start.Unix(), 10))
	costParams.Set("end_time", strconv.FormatInt(end.Unix(), 10))
	costParams.Set("bucket_width", "1d")
	costParams.Set("limit", "180")
	costParams.Add("group_by", "project_id")
	costParams.Add("group_by", "api_key_id")
	costParams.Add("group_by", "line_item")
	costParams.Add("project_ids", setting.ProjectID)
	basePath := queryPath("/organization/costs", costParams)
	path := basePath
	count := 0
	for path != "" {
		body, err := s.get(ctx, string(key), path)
		if err != nil {
			return count, fmt.Errorf("fetch OpenAI costs: %w", err)
		}
		var page struct {
			Data []struct {
				StartTime int64 `json:"start_time"`
				Results   []struct {
					Amount struct {
						Value    json.Number `json:"value"`
						Currency string      `json:"currency"`
					} `json:"amount"`
					LineItem  *string `json:"line_item"`
					ProjectID *string `json:"project_id"`
					APIKeyID  *string `json:"api_key_id"`
				} `json:"results"`
			} `json:"data"`
			HasMore  bool   `json:"has_more"`
			NextPage string `json:"next_page"`
		}
		decoder := json.NewDecoder(strings.NewReader(string(body)))
		decoder.UseNumber()
		if err := decoder.Decode(&page); err != nil {
			return count, fmt.Errorf("decode OpenAI costs: %w", ErrCostProviderResponse)
		}
		now := time.Now().UTC()
		for _, bucket := range page.Data {
			for _, item := range bucket.Results {
				project, keyID := setting.ProjectID, unattributedCostAPIKeyID
				if item.ProjectID != nil {
					project = strings.TrimSpace(*item.ProjectID)
				}
				if item.APIKeyID != nil && strings.TrimSpace(*item.APIKeyID) != "" {
					keyID = strings.TrimSpace(*item.APIKeyID)
				}
				if project != setting.ProjectID {
					continue
				}
				line := "unclassified"
				if item.LineItem != nil && *item.LineItem != "" {
					line = *item.LineItem
				}
				amount, err := money.Parse(item.Amount.Value.String())
				if err != nil {
					return count, ErrCostProviderResponse
				}
				raw, _ := json.Marshal(item)
				row := models.OpenAICostBucket{BucketDate: time.Unix(bucket.StartTime, 0).UTC(), ProjectID: project, APIKeyID: keyID, LineItem: line, ActualAmountUSD: money.Format(amount), SourceJSON: raw, Status: "open", SyncedAt: now}
				if err := s.db.WithContext(ctx).Where(models.OpenAICostBucket{BucketDate: row.BucketDate, ProjectID: project, APIKeyID: keyID, LineItem: line}).Assign(map[string]any{"actual_amount_usd": row.ActualAmountUSD, "source_json": raw, "synced_at": now}).FirstOrCreate(&row).Error; err != nil {
					return count, fmt.Errorf("persist OpenAI cost bucket: %w", err)
				}
				count++
			}
		}
		if page.HasMore && page.NextPage != "" {
			path = basePath + "&page=" + url.QueryEscape(page.NextPage)
		} else {
			path = ""
		}
	}
	if err := s.syncUsageDiagnostics(ctx, string(key), setting, start, end); err != nil {
		return count, fmt.Errorf("sync OpenAI usage diagnostics: %w", err)
	}
	if err := s.reconcile(ctx, start, end); err != nil {
		return count, fmt.Errorf("reconcile OpenAI costs: %w", err)
	}
	if err := s.db.WithContext(ctx).Model(&models.OpenAICostSetting{}).Where("id = ?", setting.ID).Update("last_synced_at", time.Now().UTC()).Error; err != nil {
		return count, fmt.Errorf("update OpenAI cost sync timestamp: %w", err)
	}
	return count, nil
}

func (s *CostService) syncUsageDiagnostics(ctx context.Context, key string, setting models.OpenAICostSetting, start, end time.Time) error {
	for _, usageType := range []string{"completions", "images"} {
		usageParams := url.Values{}
		usageParams.Set("start_time", strconv.FormatInt(start.Unix(), 10))
		usageParams.Set("end_time", strconv.FormatInt(end.Unix(), 10))
		usageParams.Set("bucket_width", "1d")
		usageParams.Set("limit", "31")
		usageParams.Add("group_by", "project_id")
		usageParams.Add("group_by", "api_key_id")
		usageParams.Add("project_ids", setting.ProjectID)
		basePath := queryPath("/organization/usage/"+usageType, usageParams)
		path := basePath
		for path != "" {
			body, err := s.get(ctx, key, path)
			if err != nil {
				return err
			}
			var page struct {
				Data     []json.RawMessage `json:"data"`
				HasMore  bool              `json:"has_more"`
				NextPage string            `json:"next_page"`
			}
			if err := json.Unmarshal(body, &page); err != nil {
				return ErrCostProviderResponse
			}
			now := time.Now().UTC()
			for _, raw := range page.Data {
				var header struct {
					StartTime int64 `json:"start_time"`
				}
				if json.Unmarshal(raw, &header) != nil || header.StartTime == 0 {
					continue
				}
				row := models.OpenAIUsageBucket{BucketDate: time.Unix(header.StartTime, 0).UTC(), ProjectID: setting.ProjectID, APIKeyID: projectScopeLegacyAPIKeyID, UsageType: usageType, SourceJSON: raw, SyncedAt: now}
				if err := s.db.WithContext(ctx).Where(models.OpenAIUsageBucket{BucketDate: row.BucketDate, ProjectID: row.ProjectID, APIKeyID: row.APIKeyID, UsageType: usageType}).Assign(map[string]any{"source_json": raw, "synced_at": now}).FirstOrCreate(&row).Error; err != nil {
					return err
				}
			}
			if page.HasMore && page.NextPage != "" {
				path = basePath + "&page=" + url.QueryEscape(page.NextPage)
			} else {
				path = ""
			}
		}
	}
	return nil
}

func (s *CostService) reconcile(ctx context.Context, start, end time.Time) error {
	var buckets []models.OpenAICostBucket
	if err := s.db.WithContext(ctx).Where("bucket_date >= ? AND bucket_date < ?", start, end).Order("bucket_date,id").Find(&buckets).Error; err != nil {
		return err
	}
	byDay := map[string][]models.OpenAICostBucket{}
	for _, b := range buckets {
		day := b.BucketDate.Format("2006-01-02")
		byDay[day] = append(byDay[day], b)
	}
	for day, dayBuckets := range byDay {
		var rows []struct {
			JobID     uint
			Estimated string
		}
		dayStart, _ := time.Parse("2006-01-02", day)
		dayEnd := dayStart.AddDate(0, 0, 1)
		if err := s.db.Table("ai_usage_ledgers AS ledger").Select("item.ai_job_id AS job_id, SUM(ledger.estimated_amount_usd) AS estimated").Joins("JOIN ai_executions AS execution ON execution.id = ledger.ai_execution_id").Joins("JOIN ai_job_items AS item ON item.id = execution.ai_job_item_id").Where("ledger.created_at >= ? AND ledger.created_at < ? AND ledger.pricing_status = ?", dayStart, dayEnd, "priced").Group("item.ai_job_id").Scan(&rows).Error; err != nil {
			return err
		}
		estimatedTotal := money.Must("0")
		for _, r := range rows {
			estimatedTotal.Add(estimatedTotal, money.Must(r.Estimated))
		}
		// Unpriced and partially priced usage remains visible in diagnostics, but
		// is excluded from the allocation basis. A day only needs attention when
		// there is no positive priced estimate to use for allocation.
		if estimatedTotal.Sign() == 0 {
			ids := make([]uint, 0, len(dayBuckets))
			for _, bucket := range dayBuckets {
				ids = append(ids, bucket.ID)
			}
			if err := s.db.Model(&models.OpenAICostBucket{}).Where("id IN ?", ids).Update("status", "needs_attention").Error; err != nil {
				return err
			}
			continue
		}
		for _, bucket := range dayBuckets {
			var maxVersion int
			_ = s.db.Model(&models.AIReconciliationAllocation{}).Where("open_ai_cost_bucket_id = ?", bucket.ID).Select("COALESCE(MAX(version),0)").Scan(&maxVersion).Error
			if maxVersion > 0 {
				var prior []models.AIReconciliationAllocation
				if err := s.db.Where("open_ai_cost_bucket_id = ? AND version = ?", bucket.ID, maxVersion).Find(&prior).Error; err != nil {
					return err
				}
				priorActual := money.Must("0")
				priorEstimated := map[uint]string{}
				for _, allocation := range prior {
					priorActual.Add(priorActual, money.Must(allocation.AllocatedAmountUSD))
					priorEstimated[allocation.AIJobID] = money.Format(money.Must(allocation.EstimatedAmountUSD))
				}
				same := priorActual.Cmp(money.Must(bucket.ActualAmountUSD)) == 0 && len(priorEstimated) == len(rows)
				for _, row := range rows {
					if priorEstimated[row.JobID] != money.Format(money.Must(row.Estimated)) {
						same = false
						break
					}
				}
				if same {
					continue
				}
			}
			version := maxVersion + 1
			actual := money.Must(bucket.ActualAmountUSD)
			allocated := money.Must("0")
			for i, r := range rows {
				amount := money.Must("0")
				if i == len(rows)-1 {
					amount.Sub(actual, allocated)
				} else {
					amount.Mul(actual, money.Must(r.Estimated))
					amount.Quo(amount, estimatedTotal)
					amount = money.Must(money.Format(amount))
					allocated.Add(allocated, amount)
				}
				entry := models.AIReconciliationAllocation{Version: version, OpenAICostBucketID: bucket.ID, AIJobID: r.JobID, EstimatedAmountUSD: r.Estimated, AllocatedAmountUSD: money.Format(amount)}
				if err := s.db.Create(&entry).Error; err != nil {
					return err
				}
			}
			if err := s.db.Model(&models.OpenAICostBucket{}).Where("id = ?", bucket.ID).Update("status", "reconciled").Error; err != nil {
				return err
			}
		}
	}
	return nil
}

type CostDaySummary struct {
	Date                string                    `json:"date"`
	EstimatedAmountUSD  string                    `json:"estimated_amount_usd"`
	ActualAmountUSD     string                    `json:"actual_amount_usd"`
	DifferenceAmountUSD string                    `json:"difference_amount_usd"`
	DifferenceRate      *string                   `json:"difference_rate"`
	UnpricedUsageCount  int64                     `json:"unpriced_usage_count"`
	Status              string                    `json:"status"`
	Buckets             []models.OpenAICostBucket `json:"buckets"`
}

func (s *CostService) Summary(ctx context.Context, start, end time.Time) ([]CostDaySummary, error) {
	var buckets []models.OpenAICostBucket
	if err := s.db.WithContext(ctx).Where("bucket_date >= ? AND bucket_date < ?", start, end).Order("bucket_date DESC,line_item").Find(&buckets).Error; err != nil {
		return nil, err
	}
	byDay := map[string]*CostDaySummary{}
	for _, bucket := range buckets {
		day := bucket.BucketDate.Format("2006-01-02")
		row := byDay[day]
		if row == nil {
			row = &CostDaySummary{Date: day, EstimatedAmountUSD: "0.00000000", ActualAmountUSD: "0.00000000", DifferenceAmountUSD: "0.00000000", Status: "pending", Buckets: []models.OpenAICostBucket{}}
			byDay[day] = row
		}
		row.Buckets = append(row.Buckets, bucket)
		row.ActualAmountUSD = money.Format(money.Add(money.Must(row.ActualAmountUSD), money.Must(bucket.ActualAmountUSD)))
		if bucket.Status == "needs_attention" {
			row.Status = "needs_attention"
		} else if bucket.Status == "reconciled" && row.Status != "needs_attention" {
			row.Status = "reconciled"
		}
	}
	result := make([]CostDaySummary, 0, len(byDay))
	for day, row := range byDay {
		dayStart, _ := time.Parse("2006-01-02", day)
		dayEnd := dayStart.AddDate(0, 0, 1)
		var estimate struct {
			Total    string
			Unpriced int64
		}
		if err := s.db.Table("ai_usage_ledgers").Select("COALESCE(SUM(CASE WHEN pricing_status = 'priced' THEN estimated_amount_usd ELSE 0 END),0) AS total, SUM(CASE WHEN pricing_status IN ('unpriced','partial') THEN 1 ELSE 0 END) AS unpriced").Where("created_at >= ? AND created_at < ?", dayStart, dayEnd).Scan(&estimate).Error; err != nil {
			return nil, err
		}
		row.EstimatedAmountUSD = money.Format(money.Must(defaultString(estimate.Total, "0")))
		row.UnpricedUsageCount = estimate.Unpriced
		difference := new(big.Rat).Sub(money.Must(row.ActualAmountUSD), money.Must(row.EstimatedAmountUSD))
		row.DifferenceAmountUSD = money.Format(difference)
		if money.Must(row.EstimatedAmountUSD).Sign() != 0 {
			rate := new(big.Rat).Quo(difference, money.Must(row.EstimatedAmountUSD))
			value := money.Format(rate)
			row.DifferenceRate = &value
		}
		result = append(result, *row)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Date > result[j].Date })
	return result, nil
}
func (s *CostService) ClosePeriod(ctx context.Context, month, invoice string, actorID uint, closed bool) (models.AIReconciliationPeriod, error) {
	if _, err := time.Parse("2006-01", month); err != nil {
		return models.AIReconciliationPeriod{}, errors.New("month must be YYYY-MM")
	}
	var row models.AIReconciliationPeriod
	err := s.db.WithContext(ctx).Where("month = ?", month).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		row = models.AIReconciliationPeriod{Month: month}
	} else if err != nil {
		return row, err
	}
	if closed {
		now := time.Now().UTC()
		row.Status = "closed"
		row.ClosedAt = &now
		row.ClosedByID = &actorID
		row.InvoiceReference = strings.TrimSpace(invoice)
	} else {
		row.Status = "open"
		row.ClosedAt = nil
		row.ClosedByID = nil
	}
	if row.ID == 0 {
		err = s.db.Create(&row).Error
	} else {
		err = s.db.Save(&row).Error
	}
	return row, err
}

func (s *CostService) active(ctx context.Context) (models.OpenAICostSetting, []byte, error) {
	var row models.OpenAICostSetting
	if err := s.db.WithContext(ctx).Where("provider = ? AND status = ?", "openai", "active").First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return row, nil, ErrCostSettingNotConfigured
		}
		return row, nil, err
	}
	plain, err := s.box.Open(secrets.EncryptedValue{Ciphertext: row.EncryptedAdminAPIKey, Nonce: row.EncryptionNonce, KeyVersion: row.EncryptionKeyVersion})
	return row, plain, err
}
func (s *CostService) get(ctx context.Context, key, path string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, s.baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+key)
	response, err := s.client.Do(request)
	if err != nil {
		log.Printf("OpenAI cost API request failed before response")
		return nil, &CostAPIError{}
	}
	defer response.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if readErr != nil {
		log.Printf("OpenAI cost API response could not be read: status=%d request_id=%q", response.StatusCode, strings.TrimSpace(response.Header.Get("x-request-id")))
		return nil, &CostAPIError{}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		requestID := strings.TrimSpace(response.Header.Get("x-request-id"))
		log.Printf("OpenAI cost API request failed: status=%d request_id=%q", response.StatusCode, requestID)
		return nil, &CostAPIError{StatusCode: response.StatusCode, RequestID: requestID}
	}
	return body, nil
}

func queryPath(endpoint string, values url.Values) string {
	return endpoint + "?" + values.Encode()
}
