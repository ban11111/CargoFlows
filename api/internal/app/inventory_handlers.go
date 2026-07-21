package app

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"sort"
	"strings"
	"time"

	"cargoflows/api/internal/models"
	"cargoflows/api/internal/money"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	inventoryDraft  = "draft"
	inventoryPosted = "posted"
)

var errInventoryConflict = errors.New("idempotency key was already used with different correction data")

var inventoryTypes = map[string]bool{
	"purchase_receipt": true, "sale_issue": true, "customer_return": true,
	"supplier_return": true, "stock_adjustment": true,
}

type inventoryTransactionInput struct {
	Type         string                 `json:"type"`
	BusinessDate string                 `json:"business_date"`
	Note         string                 `json:"note"`
	Lines        []inventoryLineInput   `json:"lines"`
	Charges      []inventoryChargeInput `json:"charges"`
}

type inventoryLineInput struct {
	SKUID           string `json:"sku_id"`
	Quantity        int    `json:"quantity"`
	SourceCurrency  string `json:"source_currency"`
	SourceUnitPrice string `json:"source_unit_price"`
	FXRateToSGD     string `json:"fx_rate_to_sgd"`
}

type inventoryChargeInput struct {
	Type           string `json:"type"`
	SourceCurrency string `json:"source_currency"`
	SourceAmount   string `json:"source_amount"`
	FXRateToSGD    string `json:"fx_rate_to_sgd"`
}

func (s *Server) createInventoryTransaction(c *gin.Context) {
	var req inventoryTransactionInput
	if err := decodeJSONStrict(c, &req); err != nil {
		respondInventoryError(c, err)
		return
	}
	value, replayed, err := s.saveInventoryDraft(c, nil, req)
	if err != nil {
		respondInventoryError(c, err)
		return
	}
	status := http.StatusCreated
	if replayed {
		status = http.StatusOK
	}
	c.JSON(status, value)
}

func (s *Server) updateInventoryTransaction(c *gin.Context) {
	var row models.InventoryTransaction
	if !isUUID(c.Param("transaction_id")) {
		respondInventoryError(c, errors.New("transaction_id must be a UUID"))
		return
	}
	if err := s.db.Where("public_id = ?", c.Param("transaction_id")).First(&row).Error; err != nil {
		respondInventoryError(c, err)
		return
	}
	var req inventoryTransactionInput
	if err := decodeJSONStrict(c, &req); err != nil {
		respondInventoryError(c, err)
		return
	}
	value, _, err := s.saveInventoryDraft(c, &row, req)
	if err != nil {
		respondInventoryError(c, err)
		return
	}
	c.JSON(http.StatusOK, value)
}

func (s *Server) saveInventoryDraft(c *gin.Context, existing *models.InventoryTransaction, req inventoryTransactionInput) (models.InventoryTransaction, bool, error) {
	value, err := normalizeInventoryInput(req)
	if err != nil {
		return models.InventoryTransaction{}, false, err
	}
	actor := currentUser(c)
	key := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if existing == nil && key != "" {
		var replay models.InventoryTransaction
		if err := s.db.Where("created_by_id = ? AND idempotency_key = ?", actor.ID, key).First(&replay).Error; err == nil {
			return s.loadInventoryTransaction(replay.PublicID)
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return models.InventoryTransaction{}, false, err
		}
	}
	date, _ := time.Parse("2006-01-02", value.BusinessDate)
	var saved models.InventoryTransaction
	err = s.db.Transaction(func(tx *gorm.DB) error {
		if existing != nil {
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&saved, existing.ID).Error; err != nil {
				return err
			}
			if saved.Status != inventoryDraft {
				return errors.New("posted inventory transactions are immutable")
			}
			if err := tx.Where("inventory_transaction_id = ?", saved.ID).Delete(&models.InventoryTransactionLine{}).Error; err != nil {
				return err
			}
			if err := tx.Where("inventory_transaction_id = ?", saved.ID).Delete(&models.InventoryCharge{}).Error; err != nil {
				return err
			}
			saved.Type, saved.BusinessDate, saved.Note = value.Type, date, value.Note
			if err := tx.Save(&saved).Error; err != nil {
				return err
			}
		} else {
			saved = models.InventoryTransaction{Type: value.Type, Status: inventoryDraft, BusinessDate: date, Note: value.Note, CreatedByID: actor.ID}
			if key != "" {
				saved.IdempotencyKey = &key
			}
			if err := tx.Create(&saved).Error; err != nil {
				return err
			}
		}
		seen := map[uint]bool{}
		for _, input := range value.Lines {
			var sku models.SKU
			if err := tx.Where("public_id = ?", input.SKUID).First(&sku).Error; err != nil {
				return errors.New("sku not found")
			}
			if seen[sku.ID] {
				return errors.New("a SKU may appear only once per inventory transaction")
			}
			seen[sku.ID] = true
			line := models.InventoryTransactionLine{InventoryTransactionID: saved.ID, SKUID: sku.ID, QuantityDelta: normalizedQuantity(value.Type, input.Quantity), SourceCurrency: input.SourceCurrency, SourceUnitPrice: input.SourceUnitPrice, FXRateToSGD: input.FXRateToSGD}
			if input.FXRateToSGD != "0.00000000" {
				line.FXRateDate = &date
				line.FXRateSource = "manual"
			}
			if err := tx.Create(&line).Error; err != nil {
				return err
			}
		}
		for _, input := range value.Charges {
			charge := models.InventoryCharge{InventoryTransactionID: saved.ID, Type: input.Type, SourceCurrency: input.SourceCurrency, SourceAmount: input.SourceAmount, FXRateToSGD: input.FXRateToSGD}
			if input.FXRateToSGD != "0.00000000" {
				charge.FXRateDate = &date
				charge.FXRateSource = "manual"
			}
			if err := tx.Create(&charge).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return models.InventoryTransaction{}, false, err
	}
	loaded, _, err := s.loadInventoryTransaction(saved.PublicID)
	return loaded, false, err
}

func normalizeInventoryInput(req inventoryTransactionInput) (inventoryTransactionInput, error) {
	req.Type = strings.TrimSpace(req.Type)
	if !inventoryTypes[req.Type] {
		return req, errors.New("unsupported inventory transaction type")
	}
	if _, err := time.Parse("2006-01-02", req.BusinessDate); err != nil {
		return req, errors.New("business_date must be YYYY-MM-DD")
	}
	if len(req.Lines) == 0 {
		return req, errors.New("at least one line is required")
	}
	if req.Type != "purchase_receipt" && len(req.Charges) != 0 {
		return req, errors.New("charges are allowed only on purchase receipts")
	}
	for index := range req.Lines {
		line := &req.Lines[index]
		if !isUUID(line.SKUID) || line.Quantity == 0 {
			return req, errors.New("each line requires a SKU UUID and non-zero quantity")
		}
		if req.Type != "stock_adjustment" && line.Quantity < 0 {
			return req, errors.New("quantity must be positive for this transaction type")
		}
		line.SourceCurrency = normalizeCurrency(line.SourceCurrency, "CNY")
		if line.SourceCurrency == "" {
			return req, errors.New("source_currency must be a three-letter ISO currency code")
		}
		if req.Type != "purchase_receipt" {
			line.SourceCurrency = "SGD"
			line.SourceUnitPrice = "0"
		}
		price, err := money.Parse(line.SourceUnitPrice)
		if err != nil || price.Sign() < 0 {
			return req, errors.New("source_unit_price must be a non-negative decimal")
		}
		rate, err := money.Parse(line.FXRateToSGD)
		if err != nil || rate.Sign() < 0 {
			return req, errors.New("fx_rate_to_sgd must be a non-negative decimal")
		}
		line.SourceUnitPrice, line.FXRateToSGD = money.Format(price), money.Format(rate)
	}
	for index := range req.Charges {
		charge := &req.Charges[index]
		charge.Type = strings.TrimSpace(charge.Type)
		if charge.Type == "" {
			return req, errors.New("charge type is required")
		}
		charge.SourceCurrency = normalizeCurrency(charge.SourceCurrency, "CNY")
		if charge.SourceCurrency == "" {
			return req, errors.New("charge source_currency must be a three-letter ISO currency code")
		}
		amount, err := money.Parse(charge.SourceAmount)
		if err != nil || amount.Sign() < 0 {
			return req, errors.New("source_amount must be non-negative")
		}
		rate, err := money.Parse(charge.FXRateToSGD)
		if err != nil || rate.Sign() < 0 {
			return req, errors.New("fx_rate_to_sgd must be non-negative")
		}
		charge.SourceAmount, charge.FXRateToSGD = money.Format(amount), money.Format(rate)
	}
	return req, nil
}

func normalizeCurrency(value, fallback string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	if value == "" {
		value = fallback
	}
	if len(value) != 3 {
		return ""
	}
	return value
}

func normalizedQuantity(kind string, quantity int) int {
	if kind == "sale_issue" || kind == "supplier_return" {
		return -quantity
	}
	return quantity
}

func (s *Server) listInventoryTransactions(c *gin.Context) {
	var rows []models.InventoryTransaction
	query := s.db.Preload("Lines.SKU.Product").Preload("Charges").Preload("CreatedBy").Preload("PostedBy").Preload("Corrections.CreatedBy").Order("business_date DESC, id DESC")
	if value := c.Query("type"); value != "" {
		query = query.Where("type = ?", value)
	}
	if value := c.Query("status"); value != "" {
		query = query.Where("status = ?", value)
	}
	if value := c.Query("sku_id"); value != "" {
		if !isUUID(value) {
			respondInventoryError(c, errors.New("sku_id must be a UUID"))
			return
		}
		query = query.Joins("JOIN inventory_transaction_lines AS filter_line ON filter_line.inventory_transaction_id = inventory_transactions.id").Joins("JOIN skus AS filter_sku ON filter_sku.id = filter_line.sk_uid").Where("filter_sku.public_id = ?", value).Distinct("inventory_transactions.*")
	}
	if err := query.Find(&rows).Error; err != nil {
		respondInventoryError(c, err)
		return
	}
	for i := range rows {
		normalizeInventoryDocument(&rows[i])
		s.decorateEffectiveInventoryCosts(&rows[i])
	}
	c.JSON(http.StatusOK, gin.H{"data": rows})
}

func (s *Server) getInventoryTransaction(c *gin.Context) {
	value, _, err := s.loadInventoryTransaction(c.Param("transaction_id"))
	if err != nil {
		respondInventoryError(c, err)
		return
	}
	c.JSON(http.StatusOK, value)
}

func (s *Server) loadInventoryTransaction(publicID string) (models.InventoryTransaction, bool, error) {
	if !isUUID(publicID) {
		return models.InventoryTransaction{}, false, errors.New("transaction_id must be a UUID")
	}
	var row models.InventoryTransaction
	err := s.db.Preload("Lines.SKU.Product").Preload("Charges").Preload("CreatedBy").Preload("PostedBy").Preload("Corrections.CreatedBy").Preload("Corrections.ResultTransaction.Lines.SKU.Product").Where("public_id = ?", publicID).First(&row).Error
	if err == nil {
		normalizeInventoryDocument(&row)
		s.decorateEffectiveInventoryCosts(&row)
	}
	return row, false, err
}

func normalizeInventoryDocument(row *models.InventoryTransaction) {
	for i := range row.Lines {
		line := &row.Lines[i]
		line.SourceUnitPrice = decimalOrZero(line.SourceUnitPrice)
		line.FXRateToSGD = decimalOrZero(line.FXRateToSGD)
		line.MerchandiseAmountSGD = decimalOrZero(line.MerchandiseAmountSGD)
		line.AllocatedChargesSGD = decimalOrZero(line.AllocatedChargesSGD)
		line.LandedUnitCostSGD = decimalOrZero(line.LandedUnitCostSGD)
		line.MovementCostSGD = decimalOrZero(line.MovementCostSGD)
		line.AverageCostBeforeSGD = decimalOrZero(line.AverageCostBeforeSGD)
		line.AverageCostAfterSGD = decimalOrZero(line.AverageCostAfterSGD)
		line.InventoryValueBeforeSGD = decimalOrZero(line.InventoryValueBeforeSGD)
		line.InventoryValueAfterSGD = decimalOrZero(line.InventoryValueAfterSGD)
		line.EffectiveMerchandiseAmountSGD = line.MerchandiseAmountSGD
		line.EffectiveAllocatedChargesSGD = line.AllocatedChargesSGD
		line.EffectiveLandedUnitCostSGD = line.LandedUnitCostSGD
		line.EffectiveMovementCostSGD = line.MovementCostSGD
		line.EffectiveAverageCostBeforeSGD = line.AverageCostBeforeSGD
		line.EffectiveAverageCostAfterSGD = line.AverageCostAfterSGD
		line.EffectiveInventoryValueBeforeSGD = line.InventoryValueBeforeSGD
		line.EffectiveInventoryValueAfterSGD = line.InventoryValueAfterSGD
		line.SKU.AverageUnitCostSGD = decimalOrZero(line.SKU.AverageUnitCostSGD)
		line.SKU.InventoryValueSGD = decimalOrZero(line.SKU.InventoryValueSGD)
	}
	for i := range row.Charges {
		charge := &row.Charges[i]
		charge.SourceAmount = decimalOrZero(charge.SourceAmount)
		charge.FXRateToSGD = decimalOrZero(charge.FXRateToSGD)
		charge.AmountSGD = decimalOrZero(charge.AmountSGD)
	}
}

func (s *Server) decorateEffectiveInventoryCosts(row *models.InventoryTransaction) {
	for i := range row.Lines {
		state, err := effectiveStateForLine(s.db, row.Lines[i])
		if err != nil {
			continue
		}
		line := &row.Lines[i]
		line.EffectiveMerchandiseAmountSGD = decimalOrZero(state.MerchandiseAmountSGD)
		line.EffectiveAllocatedChargesSGD = decimalOrZero(state.AllocatedChargesSGD)
		line.EffectiveLandedUnitCostSGD = decimalOrZero(state.LandedUnitCostSGD)
		line.EffectiveMovementCostSGD = decimalOrZero(state.MovementCostSGD)
		line.EffectiveAverageCostBeforeSGD = decimalOrZero(state.AverageCostBeforeSGD)
		line.EffectiveAverageCostAfterSGD = decimalOrZero(state.AverageCostAfterSGD)
		line.EffectiveInventoryValueBeforeSGD = decimalOrZero(state.InventoryValueBeforeSGD)
		line.EffectiveInventoryValueAfterSGD = decimalOrZero(state.InventoryValueAfterSGD)
		line.CostVersion = state.Version
	}
	for i := range row.Corrections {
		normalizeCorrection(&row.Corrections[i])
	}
}

func (s *Server) postInventoryTransaction(c *gin.Context) {
	if !isUUID(c.Param("transaction_id")) {
		respondInventoryError(c, errors.New("transaction_id must be a UUID"))
		return
	}
	var draft models.InventoryTransaction
	if err := s.db.Preload("Lines").Preload("Charges").Where("public_id = ?", c.Param("transaction_id")).First(&draft).Error; err != nil {
		respondInventoryError(c, err)
		return
	}
	if draft.Status == inventoryPosted {
		value, _, _ := s.loadInventoryTransaction(draft.PublicID)
		c.JSON(http.StatusOK, value)
		return
	}
	if err := s.prepareInventoryRates(c.Request.Context(), &draft); err != nil {
		respondInventoryError(c, err)
		return
	}
	if err := s.postPreparedInventory(&draft, currentUser(c).ID); err != nil {
		respondInventoryError(c, err)
		return
	}
	value, _, _ := s.loadInventoryTransaction(draft.PublicID)
	c.JSON(http.StatusOK, value)
}

func (s *Server) prepareInventoryRates(_ context.Context, transaction *models.InventoryTransaction) error {
	for index := range transaction.Lines {
		line := &transaction.Lines[index]
		if transaction.Type != "purchase_receipt" {
			line.FXRateToSGD = "1.00000000"
			line.FXRateDate = &transaction.BusinessDate
			line.FXRateSource = "cost_ledger"
			continue
		}
		if line.FXRateToSGD == "" || money.Must(line.FXRateToSGD).Sign() == 0 {
			rate, date, source, err := s.exchangeRate(transaction.BusinessDate, line.SourceCurrency)
			if err != nil {
				return err
			}
			line.FXRateToSGD, line.FXRateDate, line.FXRateSource = rate, &date, source
		}
	}
	for index := range transaction.Charges {
		charge := &transaction.Charges[index]
		if charge.FXRateToSGD == "" || money.Must(charge.FXRateToSGD).Sign() == 0 {
			rate, date, source, err := s.exchangeRate(transaction.BusinessDate, charge.SourceCurrency)
			if err != nil {
				return err
			}
			charge.FXRateToSGD, charge.FXRateDate, charge.FXRateSource = rate, &date, source
		}
	}
	return nil
}

func (s *Server) postPreparedInventory(prepared *models.InventoryTransaction, actorID uint) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		var transaction models.InventoryTransaction
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Preload("Lines").Preload("Charges").First(&transaction, prepared.ID).Error; err != nil {
			return err
		}
		if transaction.Status == inventoryPosted {
			return nil
		}
		if transaction.Status != inventoryDraft {
			return errors.New("inventory transaction cannot be posted")
		}
		byLine := map[uint]models.InventoryTransactionLine{}
		for _, line := range prepared.Lines {
			byLine[line.ID] = line
		}
		byCharge := map[uint]models.InventoryCharge{}
		for _, charge := range prepared.Charges {
			byCharge[charge.ID] = charge
		}
		merch := map[uint]*big.Rat{}
		totalMerch := new(big.Rat)
		totalCharges := new(big.Rat)
		for i := range transaction.Lines {
			line := &transaction.Lines[i]
			source := byLine[line.ID]
			line.FXRateToSGD, line.FXRateDate, line.FXRateSource = source.FXRateToSGD, source.FXRateDate, source.FXRateSource
			amount := money.Mul(money.Int(abs(line.QuantityDelta)), money.Must(line.SourceUnitPrice), money.Must(line.FXRateToSGD))
			merch[line.ID] = amount
			totalMerch.Add(totalMerch, amount)
		}
		for i := range transaction.Charges {
			charge := &transaction.Charges[i]
			source := byCharge[charge.ID]
			charge.FXRateToSGD, charge.FXRateDate, charge.FXRateSource = source.FXRateToSGD, source.FXRateDate, source.FXRateSource
			amount := money.Mul(money.Must(charge.SourceAmount), money.Must(charge.FXRateToSGD))
			charge.AmountSGD = money.Format(amount)
			totalCharges.Add(totalCharges, money.Must(charge.AmountSGD))
			if err := tx.Save(charge).Error; err != nil {
				return err
			}
		}
		if totalCharges.Sign() > 0 && totalMerch.Sign() == 0 {
			return errors.New("charges cannot be allocated when merchandise value is zero")
		}
		sort.Slice(transaction.Lines, func(i, j int) bool { return transaction.Lines[i].ID < transaction.Lines[j].ID })
		allocated := new(big.Rat)
		for i := range transaction.Lines {
			line := &transaction.Lines[i]
			allocation := new(big.Rat)
			if totalCharges.Sign() > 0 {
				if i == len(transaction.Lines)-1 {
					allocation.Sub(totalCharges, allocated)
				} else {
					allocation.Mul(totalCharges, merch[line.ID])
					allocation.Quo(allocation, totalMerch)
					allocation = money.Must(money.Format(allocation))
					allocated.Add(allocated, allocation)
				}
			}
			line.MerchandiseAmountSGD = money.Format(merch[line.ID])
			line.AllocatedChargesSGD = money.Format(allocation)
			var sku models.SKU
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&sku, line.SKUID).Error; err != nil {
				return err
			}
			beforeAvg, beforeValue := money.Must(sku.AverageUnitCostSGD), money.Must(sku.InventoryValueSGD)
			afterQty := sku.Stock + line.QuantityDelta
			if afterQty < 0 {
				return errors.New("stock cannot be negative")
			}
			line.QuantityBefore, line.QuantityAfter = sku.Stock, afterQty
			line.AverageCostBeforeSGD, line.InventoryValueBeforeSGD = money.Format(beforeAvg), money.Format(beforeValue)
			afterAvg, afterValue := new(big.Rat).Set(beforeAvg), new(big.Rat).Set(beforeValue)
			if line.QuantityDelta > 0 && transaction.Type == "purchase_receipt" {
				movement := money.Add(merch[line.ID], allocation)
				line.MovementCostSGD = money.Format(movement)
				line.LandedUnitCostSGD = money.Format(new(big.Rat).Quo(movement, money.Int(line.QuantityDelta)))
				afterValue.Add(afterValue, movement)
				if afterQty > 0 {
					afterAvg.Quo(afterValue, money.Int(afterQty))
				}
			} else {
				movement := money.Mul(money.Int(abs(line.QuantityDelta)), beforeAvg)
				line.MovementCostSGD = money.Format(movement)
				line.LandedUnitCostSGD = money.Format(beforeAvg)
				if line.QuantityDelta > 0 {
					afterValue.Add(afterValue, movement)
				} else {
					afterValue.Sub(afterValue, movement)
				}
			}
			if afterValue.Sign() < 0 && afterQty == 0 {
				afterValue.SetInt64(0)
			}
			line.AverageCostAfterSGD, line.InventoryValueAfterSGD = money.Format(afterAvg), money.Format(afterValue)
			sku.Stock = afterQty
			sku.AverageUnitCostSGD = line.AverageCostAfterSGD
			sku.InventoryValueSGD = line.InventoryValueAfterSGD
			if afterAvg.Sign() > 0 {
				sku.ZeroCostOpening = false
			}
			if err := tx.Save(&sku).Error; err != nil {
				return err
			}
			if err := tx.Save(line).Error; err != nil {
				return err
			}
		}
		now := time.Now().UTC()
		transaction.Status = inventoryPosted
		transaction.PostedAt = &now
		transaction.PostedByID = &actorID
		return tx.Save(&transaction).Error
	})
}

func (s *Server) reverseInventoryTransaction(c *gin.Context) {
	// Compatibility route: legacy callers now receive the same guarded exact
	// reversal used by the unified correction API.
	key := "legacy-reverse:" + c.Param("transaction_id")
	result, err := s.inventoryCorrection(c, inventoryCorrectionInput{Kind: "void", Reason: "Legacy reversal request"}, true, key)
	if err != nil {
		respondInventoryError(c, err)
		return
	}
	if result.Correction == nil || result.Correction.ResultTransactionID == nil {
		respondInventoryError(c, errors.New("exact reversal did not create a result transaction"))
		return
	}
	var reversal models.InventoryTransaction
	if err := s.db.First(&reversal, *result.Correction.ResultTransactionID).Error; err != nil {
		respondInventoryError(c, err)
		return
	}
	value, _, err := s.loadInventoryTransaction(reversal.PublicID)
	if err != nil {
		respondInventoryError(c, err)
		return
	}
	c.JSON(http.StatusCreated, value)
}

func (s *Server) exchangeRate(date time.Time, currency string) (string, time.Time, string, error) {
	if currency == "SGD" {
		return "1.00000000", date, "identity", nil
	}
	var cached models.ExchangeRate
	if err := s.db.Where("source_currency = ? AND target_currency = ? AND rate_date <= ?", currency, "SGD", date).Order("rate_date DESC").First(&cached).Error; err == nil {
		return cached.Rate, cached.RateDate, cached.Source, nil
	}
	rates, rateDate, err := fetchECBRates(date)
	if err != nil {
		return "", time.Time{}, "", err
	}
	source, ok := rates[currency]
	if !ok {
		return "", time.Time{}, "", errors.New("ECB does not publish the requested currency")
	}
	sgd, ok := rates["SGD"]
	if !ok {
		return "", time.Time{}, "", errors.New("ECB SGD rate unavailable")
	}
	rate := new(big.Rat).Quo(sgd, source)
	row := models.ExchangeRate{RateDate: rateDate, SourceCurrency: currency, TargetCurrency: "SGD", Rate: money.Format(rate), Source: "ecb"}
	_ = s.db.Where(models.ExchangeRate{RateDate: rateDate, SourceCurrency: currency, TargetCurrency: "SGD"}).Assign(row).FirstOrCreate(&row).Error
	return row.Rate, rateDate, row.Source, nil
}

type ecbEnvelope struct {
	Cubes []struct {
		Time  string `xml:"time,attr"`
		Rates []struct {
			Currency string `xml:"currency,attr"`
			Rate     string `xml:"rate,attr"`
		} `xml:"Cube"`
	} `xml:"Cube>Cube"`
}

func fetchECBRates(target time.Time) (map[string]*big.Rat, time.Time, error) {
	resp, err := http.Get("https://www.ecb.europa.eu/stats/eurofxref/eurofxref-hist.xml")
	if err != nil {
		return nil, time.Time{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, time.Time{}, fmt.Errorf("ECB returned %s", resp.Status)
	}
	return parseECBRates(resp.Body, target)
}

func parseECBRates(reader io.Reader, target time.Time) (map[string]*big.Rat, time.Time, error) {
	var doc ecbEnvelope
	if err := xml.NewDecoder(reader).Decode(&doc); err != nil {
		return nil, time.Time{}, err
	}
	for _, cube := range doc.Cubes {
		date, err := time.Parse("2006-01-02", cube.Time)
		if err != nil || date.After(target) {
			continue
		}
		rates := map[string]*big.Rat{"EUR": money.Int(1)}
		for _, item := range cube.Rates {
			value, err := money.Parse(item.Rate)
			if err == nil {
				rates[item.Currency] = value
			}
		}
		return rates, date, nil
	}
	return nil, time.Time{}, errors.New("no ECB rate is available for the business date")
}

func respondInventoryError(c *gin.Context, err error) {
	status := http.StatusBadRequest
	if errors.Is(err, gorm.ErrRecordNotFound) {
		status = http.StatusNotFound
	} else if errors.Is(err, errInventoryConflict) {
		status = http.StatusConflict
	}
	c.JSON(status, gin.H{"code": "inventory_error", "message": err.Error()})
}
func abs(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
