package ai

import (
	"testing"
	"time"

	"cargoflows/api/internal/models"
	"cargoflows/api/internal/money"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestPriceUsageLedgerFreezesMetricBreakdownWithoutDoubleChargingReasoning(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.AIExecution{}, &models.AIUsageLedger{}, &models.AIRateCard{}, &models.AIUsageCharge{}); err != nil {
		t.Fatal(err)
	}
	effective := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	rates := []models.AIRateCard{
		{Version: 1, Model: "gpt-test", APIMode: "responses", ServiceTier: "default", Metric: "input_text", UnitSize: 1_000_000, UnitRateUSD: "10.00000000", EffectiveAt: effective, CreatedByID: 1},
		{Version: 1, Model: "gpt-test", APIMode: "responses", ServiceTier: "default", Metric: "cached_input", UnitSize: 1_000_000, UnitRateUSD: "2.00000000", EffectiveAt: effective, CreatedByID: 1},
		{Version: 1, Model: "gpt-test", APIMode: "responses", ServiceTier: "default", Metric: "output_text", UnitSize: 1_000_000, UnitRateUSD: "20.00000000", EffectiveAt: effective, CreatedByID: 1},
	}
	for i := range rates {
		if err := db.Create(&rates[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	execution := models.AIExecution{PublicID: uuid.NewString(), AIJobItemID: 1, Operation: models.AIExecutionTextGenerate, Status: models.AIExecutionCompleted, AttemptNumber: 1, Model: "gpt-test", ActualModel: "gpt-test", APIMode: "responses", ServiceTier: "default", NormalizedInputJSON: []byte(`{}`), OrderedInputListJSON: []byte(`[]`), RequestConfigJSON: []byte(`{}`)}
	if err := db.Create(&execution).Error; err != nil {
		t.Fatal(err)
	}
	ledger := models.AIUsageLedger{AIExecutionID: execution.ID, Model: "gpt-test", InputTextTokens: 1000, CachedInputTokens: 200, OutputTextTokens: 500, ReasoningTokens: 100, TotalTokens: 1500, Currency: "USD", CreatedAt: effective.Add(time.Hour)}
	if err := db.Create(&ledger).Error; err != nil {
		t.Fatal(err)
	}
	if err := PriceUsageLedger(db, &ledger, execution); err != nil {
		t.Fatal(err)
	}
	if ledger.PricingStatus != "priced" || ledger.EstimatedAmountUSD != "0.01840000" {
		t.Fatalf("ledger=%+v", ledger)
	}
	var charges []models.AIUsageCharge
	if err := db.Where("ai_usage_ledger_id = ?", ledger.ID).Order("metric").Find(&charges).Error; err != nil {
		t.Fatal(err)
	}
	if len(charges) != 3 {
		t.Fatalf("charges=%+v", charges)
	}
	for _, charge := range charges {
		if charge.Metric == "reasoning" {
			t.Fatal("reasoning must remain an output subset, not a separate charge")
		}
	}
	var cached models.AIUsageCharge
	if err := db.Where("ai_usage_ledger_id = ? AND metric = ?", ledger.ID, "cached_input").First(&cached).Error; err != nil {
		t.Fatal(err)
	}
	if cached.Quantity != 200 || money.Format(money.Must(cached.AmountUSD)) != "0.00040000" {
		t.Fatalf("cached=%+v", cached)
	}
	if err := db.First(&execution, execution.ID).Error; err != nil {
		t.Fatal(err)
	}
	if execution.PricingStatus != "priced" || money.Format(money.Must(execution.EstimatedAmountUSD)) != "0.01840000" {
		t.Fatalf("execution=%+v", execution)
	}
}

func TestPriceUsageLedgerMarksMissingRateUnpriced(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.AIExecution{}, &models.AIUsageLedger{}, &models.AIRateCard{}, &models.AIUsageCharge{}); err != nil {
		t.Fatal(err)
	}
	execution := models.AIExecution{PublicID: uuid.NewString(), AIJobItemID: 1, Operation: models.AIExecutionTextGenerate, Status: models.AIExecutionCompleted, AttemptNumber: 1, Model: "unknown", ActualModel: "unknown", APIMode: "responses", ServiceTier: "default", NormalizedInputJSON: []byte(`{}`), OrderedInputListJSON: []byte(`[]`), RequestConfigJSON: []byte(`{}`)}
	db.Create(&execution)
	ledger := models.AIUsageLedger{AIExecutionID: execution.ID, Model: "unknown", InputTextTokens: 10, TotalTokens: 10, Currency: "USD", CreatedAt: time.Now().UTC()}
	db.Create(&ledger)
	if err := PriceUsageLedger(db, &ledger, execution); err != nil {
		t.Fatal(err)
	}
	if ledger.PricingStatus != "unpriced" || ledger.EstimatedAmountUSD != "0.00000000" {
		t.Fatalf("ledger=%+v", ledger)
	}
}
