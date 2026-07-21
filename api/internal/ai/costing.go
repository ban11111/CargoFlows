package ai

import (
	"errors"
	"time"

	"cargoflows/api/internal/models"
	"cargoflows/api/internal/money"
	"gorm.io/gorm"
)

var usageMetrics = []string{"input_text", "cached_input", "input_image", "output_text", "output_image"}

func PriceUsageLedger(tx *gorm.DB, ledger *models.AIUsageLedger, execution models.AIExecution) error {
	// Some maintenance and isolated-test databases intentionally migrate only
	// the legacy execution tables. Usage capture must remain available there;
	// pricing begins once the rate/charge tables are migrated.
	if !tx.Migrator().HasTable(&models.AIRateCard{}) || !tx.Migrator().HasTable(&models.AIUsageCharge{}) {
		ledger.PricingStatus = "unpriced"
		ledger.EstimatedAmountUSD = "0.00000000"
		if err := tx.Model(ledger).Updates(map[string]any{"pricing_status": ledger.PricingStatus, "estimated_amount_usd": ledger.EstimatedAmountUSD}).Error; err != nil {
			return err
		}
		return tx.Model(&models.AIExecution{}).Where("id = ?", execution.ID).Updates(map[string]any{"pricing_status": ledger.PricingStatus, "estimated_amount_usd": ledger.EstimatedAmountUSD}).Error
	}
	quantities := map[string]int64{
		"input_text":   max64(0, ledger.InputTextTokens-ledger.CachedInputTokens),
		"cached_input": ledger.CachedInputTokens,
		"input_image":  ledger.InputImageTokens,
		"output_text":  ledger.OutputTextTokens,
		"output_image": ledger.OutputImageTokens,
	}
	when := ledger.CreatedAt
	if when.IsZero() {
		when = time.Now().UTC()
	}
	total := money.Must("0")
	priced, required := 0, 0
	var existing []models.AIUsageCharge
	if err := tx.Where("ai_usage_ledger_id = ?", ledger.ID).Find(&existing).Error; err != nil {
		return err
	}
	byMetric := make(map[string]models.AIUsageCharge, len(existing))
	for _, charge := range existing {
		byMetric[charge.Metric] = charge
	}
	for _, metric := range usageMetrics {
		quantity := quantities[metric]
		if quantity == 0 {
			continue
		}
		required++
		if frozen, ok := byMetric[metric]; ok {
			total.Add(total, money.Must(frozen.AmountUSD))
			priced++
			continue
		}
		var rate models.AIRateCard
		err := tx.Where("model = ? AND api_mode = ? AND service_tier = ? AND metric = ? AND effective_at <= ?", ledger.Model, defaultString(execution.APIMode, "responses"), defaultString(execution.ServiceTier, "default"), metric, when).Order("effective_at DESC, version DESC").First(&rate).Error
		if errors.Is(err, gorm.ErrRecordNotFound) && execution.ServiceTier != "default" {
			err = tx.Where("model = ? AND api_mode = ? AND service_tier = ? AND metric = ? AND effective_at <= ?", ledger.Model, defaultString(execution.APIMode, "responses"), "default", metric, when).Order("effective_at DESC, version DESC").First(&rate).Error
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		amount := money.Mul(money.Int(int(quantity)), money.Must(rate.UnitRateUSD))
		amount.Quo(amount, money.Int(int(rate.UnitSize)))
		amount = money.Must(money.Format(amount))
		charge := models.AIUsageCharge{AIUsageLedgerID: ledger.ID, AIRateCardID: rate.ID, Metric: metric, Quantity: quantity, UnitSize: rate.UnitSize, UnitRateUSD: rate.UnitRateUSD, AmountUSD: money.Format(amount)}
		if err := tx.Create(&charge).Error; err != nil {
			return err
		}
		total.Add(total, amount)
		priced++
	}
	status := "unpriced"
	if required == 0 || priced == required {
		status = "priced"
	} else if priced > 0 {
		status = "partial"
	}
	ledger.PricingStatus = status
	ledger.EstimatedAmountUSD = money.Format(total)
	if err := tx.Model(ledger).Updates(map[string]any{"pricing_status": status, "estimated_amount_usd": ledger.EstimatedAmountUSD}).Error; err != nil {
		return err
	}
	updates := map[string]any{"pricing_status": status, "estimated_amount_usd": ledger.EstimatedAmountUSD}
	if status == "priced" {
		// Keep the legacy float column populated for old clients. The decimal
		// amount above remains the accounting source of truth.
		value, _ := total.Float64()
		updates["estimated_cost"], updates["currency"] = value, "USD"
	}
	return tx.Model(&models.AIExecution{}).Where("id = ?", execution.ID).Updates(updates).Error
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
