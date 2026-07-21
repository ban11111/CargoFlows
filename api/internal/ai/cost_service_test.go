package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"cargoflows/api/internal/models"
	"cargoflows/api/internal/money"
	"cargoflows/api/internal/secrets"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestCostSyncUsesProjectScopeAndUsageLimitsAcrossKeyRotation(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&models.OpenAICostSetting{}, &models.OpenAICostBucket{}, &models.OpenAIUsageBucket{},
		&models.AIReconciliationPeriod{}, &models.AIReconciliationAllocation{},
		&models.AIJob{}, &models.AIJobItem{}, &models.AIExecution{}, &models.AIUsageLedger{},
	); err != nil {
		t.Fatal(err)
	}

	projectID := "proj_rotation"
	day := time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)
	var requests []string
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.RequestURI())
		query := r.URL.Query()
		if query.Get("api_key_ids") != "" {
			t.Errorf("%s unexpectedly filtered api_key_ids=%q", r.URL.Path, query.Get("api_key_ids"))
		}
		if got := query["project_ids"]; !reflect.DeepEqual(got, []string{projectID}) {
			t.Errorf("%s project_ids=%v", r.URL.Path, got)
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/organization/costs":
			if query.Get("limit") == "1" {
				_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{}, "has_more": false})
				return
			}
			if query.Get("limit") != "180" {
				t.Errorf("cost limit=%q", query.Get("limit"))
			}
			if got := query["group_by"]; !reflect.DeepEqual(got, []string{"project_id", "api_key_id", "line_item"}) {
				t.Errorf("cost group_by=%v", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{
				"start_time": day.Unix(),
				"results": []any{
					map[string]any{"amount": map[string]any{"value": 1.00, "currency": "usd"}, "line_item": "responses", "project_id": projectID, "api_key_id": "key_old"},
					map[string]any{"amount": map[string]any{"value": 0.25, "currency": "usd"}, "line_item": "responses", "project_id": projectID, "api_key_id": "key_new"},
					map[string]any{"amount": map[string]any{"value": 0.04, "currency": "usd"}, "line_item": "storage", "project_id": projectID, "api_key_id": nil},
				},
			}}, "has_more": false})
		case "/organization/usage/completions", "/organization/usage/images":
			if query.Get("limit") != "31" {
				t.Errorf("usage limit=%q", query.Get("limit"))
			}
			if got := query["group_by"]; !reflect.DeepEqual(got, []string{"project_id", "api_key_id"}) {
				t.Errorf("usage group_by=%v", got)
			}
			if r.URL.Path == "/organization/usage/images" {
				_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{}, "has_more": false})
				return
			}
			if query.Get("page") == "" {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"data": []any{map[string]any{
						"start_time": day.Unix(),
						"results":    []any{map[string]any{"project_id": projectID, "api_key_id": "key_old"}},
					}},
					"has_more": true, "next_page": "next cursor",
				})
				return
			}
			if query.Get("page") != "next cursor" {
				t.Errorf("usage page=%q", query.Get("page"))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []any{map[string]any{
					"start_time": day.AddDate(0, 0, 1).Unix(),
					"results":    []any{map[string]any{"project_id": projectID, "api_key_id": "key_new"}},
				}},
				"has_more": false,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer provider.Close()

	box, err := secrets.NewAESGCM(bytes.Repeat([]byte{0x41}, 32))
	if err != nil {
		t.Fatal(err)
	}
	service := NewCostService(db, box, provider.URL, provider.Client())
	setting, err := service.Configure(t.Context(), 7, "sk-admin-project-scope-secret", projectID, "key_legacy_ignored")
	if err != nil {
		t.Fatal(err)
	}
	if setting.Scope != CostScopeProject || setting.APIKeyID != "" {
		t.Fatalf("setting=%+v", setting)
	}

	for iteration := 0; iteration < 2; iteration++ {
		if _, err := service.Sync(t.Context(), day, day.AddDate(0, 0, 2)); err != nil {
			t.Fatal(err)
		}
	}
	var costBuckets []models.OpenAICostBucket
	if err := db.Order("api_key_id,line_item").Find(&costBuckets).Error; err != nil {
		t.Fatal(err)
	}
	if len(costBuckets) != 3 {
		t.Fatalf("cost buckets=%d: %+v", len(costBuckets), costBuckets)
	}
	keyIDs := []string{costBuckets[0].APIKeyID, costBuckets[1].APIKeyID, costBuckets[2].APIKeyID}
	if !reflect.DeepEqual(keyIDs, []string{"key_new", "key_old", unattributedCostAPIKeyID}) {
		t.Fatalf("api key ids=%v", keyIDs)
	}
	var usageCount int64
	if err := db.Model(&models.OpenAIUsageBucket{}).Count(&usageCount).Error; err != nil || usageCount != 2 {
		t.Fatalf("usage count=%d err=%v", usageCount, err)
	}
	if !containsRequest(requests, "/organization/usage/completions", "page=next+cursor") {
		t.Fatalf("pagination request missing: %v", requests)
	}
}

func TestCostAPIErrorDoesNotExposeProviderBody(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("x-request-id", "req_safe")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"sk-admin-secret-must-not-leak"}`))
	}))
	defer provider.Close()
	service := NewCostService(nil, nil, provider.URL, provider.Client())
	_, err := service.get(t.Context(), "sk-admin-secret-must-not-leak", "/organization/costs?start_time=1")
	var providerErr *CostAPIError
	if !errors.As(err, &providerErr) || providerErr.StatusCode != http.StatusForbidden || providerErr.RequestID != "req_safe" {
		t.Fatalf("error=%#v", err)
	}
	if strings.Contains(err.Error(), "secret") {
		t.Fatalf("provider body leaked: %v", err)
	}
}

func containsRequest(requests []string, path, queryPart string) bool {
	for _, request := range requests {
		if strings.HasPrefix(request, path+"?") && strings.Contains(request, queryPart) {
			return true
		}
	}
	return false
}

func TestReconciliationAllocationsConserveBucketsAndVersionOnlyOnChange(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.AIJob{}, &models.AIJobItem{}, &models.AIExecution{}, &models.AIUsageLedger{}, &models.OpenAICostBucket{}, &models.AIReconciliationAllocation{}); err != nil {
		t.Fatal(err)
	}
	day := time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)
	jobA := createReconciliationJob(t, db, day.Add(time.Hour), "1.00000000", "priced")
	jobB := createReconciliationJob(t, db, day.Add(2*time.Hour), "3.00000000", "priced")
	bucket := models.OpenAICostBucket{BucketDate: day, ProjectID: "proj", APIKeyID: "key", LineItem: "responses", ActualAmountUSD: "1.00000000", SourceJSON: []byte(`{}`), Status: "open", SyncedAt: day.Add(3 * time.Hour)}
	if err := db.Create(&bucket).Error; err != nil {
		t.Fatal(err)
	}
	service := &CostService{db: db}
	if err := service.reconcile(context.Background(), day, day.AddDate(0, 0, 1)); err != nil {
		t.Fatal(err)
	}
	assertAllocation(t, db, bucket.ID, 1, jobA.ID, "0.25000000")
	assertAllocation(t, db, bucket.ID, 1, jobB.ID, "0.75000000")
	if err := service.reconcile(context.Background(), day, day.AddDate(0, 0, 1)); err != nil {
		t.Fatal(err)
	}
	var count int64
	db.Model(&models.AIReconciliationAllocation{}).Where("open_ai_cost_bucket_id = ?", bucket.ID).Count(&count)
	if count != 2 {
		t.Fatalf("unchanged reconciliation created %d rows", count)
	}
	if err := db.Model(&bucket).Update("actual_amount_usd", "2.00000000").Error; err != nil {
		t.Fatal(err)
	}
	if err := service.reconcile(context.Background(), day, day.AddDate(0, 0, 1)); err != nil {
		t.Fatal(err)
	}
	assertAllocation(t, db, bucket.ID, 2, jobA.ID, "0.50000000")
	assertAllocation(t, db, bucket.ID, 2, jobB.ID, "1.50000000")
}

func TestReconciliationStopsAllocationWhenUsageIsUnpriced(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.AIJob{}, &models.AIJobItem{}, &models.AIExecution{}, &models.AIUsageLedger{}, &models.OpenAICostBucket{}, &models.AIReconciliationAllocation{}); err != nil {
		t.Fatal(err)
	}
	day := time.Date(2026, 7, 21, 0, 0, 0, 0, time.UTC)
	createReconciliationJob(t, db, day.Add(time.Hour), "1.00000000", "priced")
	createReconciliationJob(t, db, day.Add(2*time.Hour), "0.00000000", "unpriced")
	bucket := models.OpenAICostBucket{BucketDate: day, ProjectID: "proj", APIKeyID: "key", LineItem: "images", ActualAmountUSD: "2.00000000", SourceJSON: []byte(`{}`), Status: "open", SyncedAt: day}
	db.Create(&bucket)
	if err := (&CostService{db: db}).reconcile(context.Background(), day, day.AddDate(0, 0, 1)); err != nil {
		t.Fatal(err)
	}
	var count int64
	db.Model(&models.AIReconciliationAllocation{}).Where("open_ai_cost_bucket_id = ?", bucket.ID).Count(&count)
	if count != 0 {
		t.Fatalf("allocations=%d", count)
	}
	db.First(&bucket, bucket.ID)
	if bucket.Status != "needs_attention" {
		t.Fatalf("status=%s", bucket.Status)
	}
}

func createReconciliationJob(t *testing.T, db *gorm.DB, at time.Time, estimate, status string) models.AIJob {
	t.Helper()
	job := models.AIJob{PublicID: uuid.NewString(), SKUID: 1, AIContentTemplateVersionID: 1, TargetPlatform: "test", Locale: "en", Status: models.AIJobCompleted, SnapshotSchema: "test", InputSnapshotJSON: []byte(`{}`), CreatedByID: 1}
	if err := db.Create(&job).Error; err != nil {
		t.Fatal(err)
	}
	item := models.AIJobItem{PublicID: uuid.NewString(), AIJobID: job.ID, AIContentSlotID: 1, SlotKey: uuid.NewString(), Kind: models.AIContentSlotTitle, Status: models.AIJobItemCompleted, SlotSnapshotJSON: []byte(`{}`), SelectedInputAssetIDsJSON: []byte(`[]`)}
	if err := db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	execution := models.AIExecution{PublicID: uuid.NewString(), AIJobItemID: item.ID, Operation: models.AIExecutionTextGenerate, Status: models.AIExecutionCompleted, AttemptNumber: 1, Model: "gpt", NormalizedInputJSON: []byte(`{}`), OrderedInputListJSON: []byte(`[]`), RequestConfigJSON: []byte(`{}`)}
	if err := db.Create(&execution).Error; err != nil {
		t.Fatal(err)
	}
	ledger := models.AIUsageLedger{AIExecutionID: execution.ID, Model: "gpt", InputTextTokens: 1, TotalTokens: 1, Currency: "USD", PricingStatus: status, EstimatedAmountUSD: estimate, CreatedAt: at}
	if err := db.Create(&ledger).Error; err != nil {
		t.Fatal(err)
	}
	return job
}

func assertAllocation(t *testing.T, db *gorm.DB, bucketID uint, version int, jobID uint, expected string) {
	t.Helper()
	var row models.AIReconciliationAllocation
	if err := db.Where("open_ai_cost_bucket_id = ? AND version = ? AND ai_job_id = ?", bucketID, version, jobID).First(&row).Error; err != nil {
		t.Fatal(err)
	}
	if money.Format(money.Must(row.AllocatedAmountUSD)) != expected {
		t.Fatalf("allocation=%s want=%s", row.AllocatedAmountUSD, expected)
	}
}
