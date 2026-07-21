package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
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

type inventoryCorrectionInput struct {
	Kind    string                 `json:"kind"`
	Reason  string                 `json:"reason"`
	Lines   []inventoryLineInput   `json:"lines"`
	Charges []inventoryChargeInput `json:"charges"`
}

type inventoryCorrectionImpact struct {
	SKUID                         string `json:"sku_id"`
	SKUCode                       string `json:"sku_code"`
	CurrentAverageCostSGD         string `json:"current_average_cost_sgd"`
	EffectiveAverageCostSGD       string `json:"effective_average_cost_sgd"`
	CurrentInventoryValueSGD      string `json:"current_inventory_value_sgd"`
	EffectiveInventoryValueSGD    string `json:"effective_inventory_value_sgd"`
	InventoryValueDeltaSGD        string `json:"inventory_value_delta_sgd"`
	HistoricalOutflowCostDeltaSGD string `json:"historical_outflow_cost_delta_sgd"`
	AffectedTransactionCount      int    `json:"affected_transaction_count"`
}

type inventoryCorrectionResult struct {
	Strategy                      string                      `json:"strategy"`
	InventoryValueDeltaSGD        string                      `json:"inventory_value_delta_sgd"`
	HistoricalOutflowCostDeltaSGD string                      `json:"historical_outflow_cost_delta_sgd"`
	Impacts                       []inventoryCorrectionImpact `json:"impacts"`
	Correction                    *models.InventoryCorrection `json:"correction,omitempty"`
}

type effectiveCostState struct {
	MerchandiseAmountSGD    string
	AllocatedChargesSGD     string
	LandedUnitCostSGD       string
	MovementCostSGD         string
	AverageCostBeforeSGD    string
	AverageCostAfterSGD     string
	InventoryValueBeforeSGD string
	InventoryValueAfterSGD  string
	Version                 int
}

type purchaseCostValue struct {
	Merchandise string
	Allocated   string
	Landed      string
	Movement    string
}

type costCorrectionPlan struct {
	Prepared       models.InventoryTransaction
	Values         map[uint]purchaseCostValue
	Lines          []models.InventoryCorrectionLine
	Charges        []models.InventoryCorrectionCharge
	Revisions      []models.InventoryCostRevision
	Impacts        []inventoryCorrectionImpact
	InventoryDelta *big.Rat
	OutflowDelta   *big.Rat
	EndingStates   map[uint]effectiveCostState
}

func (s *Server) previewInventoryCorrection(c *gin.Context) {
	var req inventoryCorrectionInput
	if err := decodeJSONStrict(c, &req); err != nil {
		respondInventoryError(c, err)
		return
	}
	result, err := s.inventoryCorrection(c, req, false, "")
	if err != nil {
		respondInventoryError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (s *Server) createInventoryCorrection(c *gin.Context) {
	key := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if len(key) < 8 || len(key) > 128 {
		respondInventoryError(c, errors.New("Idempotency-Key must contain 8 to 128 characters"))
		return
	}
	var req inventoryCorrectionInput
	if err := decodeJSONStrict(c, &req); err != nil {
		respondInventoryError(c, err)
		return
	}
	result, err := s.inventoryCorrection(c, req, true, key)
	if err != nil {
		respondInventoryError(c, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}

func (s *Server) listInventoryCorrections(c *gin.Context) {
	if !isUUID(c.Param("transaction_id")) {
		respondInventoryError(c, errors.New("transaction_id must be a UUID"))
		return
	}
	var original models.InventoryTransaction
	if err := s.db.Where("public_id = ?", c.Param("transaction_id")).First(&original).Error; err != nil {
		respondInventoryError(c, err)
		return
	}
	var rows []models.InventoryCorrection
	if err := correctionPreloads(s.db).Where("original_transaction_id = ?", original.ID).Order("version DESC").Find(&rows).Error; err != nil {
		respondInventoryError(c, err)
		return
	}
	for i := range rows {
		normalizeCorrection(&rows[i])
	}
	c.JSON(http.StatusOK, gin.H{"data": rows})
}

func correctionPreloads(db *gorm.DB) *gorm.DB {
	return db.Preload("CreatedBy").Preload("ResultTransaction.Lines.SKU.Product").Preload("Lines.SKU.Product").Preload("Charges").Preload("Revisions.InventoryTransactionLine.SKU.Product")
}

func normalizeCorrection(value *models.InventoryCorrection) {
	value.InventoryValueDeltaSGD = decimalOrZero(value.InventoryValueDeltaSGD)
	value.HistoricalOutflowCostDeltaSGD = decimalOrZero(value.HistoricalOutflowCostDeltaSGD)
	for i := range value.Lines {
		value.Lines[i].SourceUnitPrice = decimalOrZero(value.Lines[i].SourceUnitPrice)
		value.Lines[i].FXRateToSGD = decimalOrZero(value.Lines[i].FXRateToSGD)
	}
	for i := range value.Charges {
		value.Charges[i].SourceAmount = decimalOrZero(value.Charges[i].SourceAmount)
		value.Charges[i].FXRateToSGD = decimalOrZero(value.Charges[i].FXRateToSGD)
	}
	if value.ResultTransaction != nil {
		normalizeInventoryDocument(value.ResultTransaction)
	}
	for i := range value.Revisions {
		revision := &value.Revisions[i]
		revision.EffectiveMerchandiseAmountSGD = decimalOrZero(revision.EffectiveMerchandiseAmountSGD)
		revision.EffectiveAllocatedChargesSGD = decimalOrZero(revision.EffectiveAllocatedChargesSGD)
		revision.EffectiveLandedUnitCostSGD = decimalOrZero(revision.EffectiveLandedUnitCostSGD)
		revision.EffectiveMovementCostSGD = decimalOrZero(revision.EffectiveMovementCostSGD)
		revision.EffectiveAverageCostBeforeSGD = decimalOrZero(revision.EffectiveAverageCostBeforeSGD)
		revision.EffectiveAverageCostAfterSGD = decimalOrZero(revision.EffectiveAverageCostAfterSGD)
		revision.EffectiveInventoryValueBeforeSGD = decimalOrZero(revision.EffectiveInventoryValueBeforeSGD)
		revision.EffectiveInventoryValueAfterSGD = decimalOrZero(revision.EffectiveInventoryValueAfterSGD)
		document := models.InventoryTransaction{Lines: []models.InventoryTransactionLine{revision.InventoryTransactionLine}}
		normalizeInventoryDocument(&document)
		revision.InventoryTransactionLine = document.Lines[0]
	}
}

func normalizeCorrectionInput(req inventoryCorrectionInput) (inventoryCorrectionInput, error) {
	req.Kind = strings.TrimSpace(req.Kind)
	req.Reason = strings.TrimSpace(req.Reason)
	if req.Kind != "void" && req.Kind != "cost" && req.Kind != "quantity" {
		return req, errors.New("kind must be void, cost, or quantity")
	}
	if req.Reason == "" {
		return req, errors.New("reason is required")
	}
	if req.Kind == "void" && (len(req.Lines) != 0 || len(req.Charges) != 0) {
		return req, errors.New("void corrections do not accept lines or charges")
	}
	return req, nil
}

func (s *Server) inventoryCorrection(c *gin.Context, raw inventoryCorrectionInput, persist bool, key string) (inventoryCorrectionResult, error) {
	req, err := normalizeCorrectionInput(raw)
	if err != nil {
		return inventoryCorrectionResult{}, err
	}
	if !isUUID(c.Param("transaction_id")) {
		return inventoryCorrectionResult{}, errors.New("transaction_id must be a UUID")
	}
	actor := currentUser(c)
	fingerprint, err := correctionFingerprint(req)
	if err != nil {
		return inventoryCorrectionResult{}, err
	}
	if persist {
		var replay models.InventoryCorrection
		if err := correctionPreloads(s.db).Where("created_by_id = ? AND idempotency_key = ?", actor.ID, key).First(&replay).Error; err == nil {
			if replay.RequestFingerprint != fingerprint {
				return inventoryCorrectionResult{}, errInventoryConflict
			}
			normalizeCorrection(&replay)
			return correctionResultFromModel(replay), nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return inventoryCorrectionResult{}, err
		}
	}
	var original models.InventoryTransaction
	if err := s.db.Preload("Lines.SKU.Product").Preload("Charges").Where("public_id = ?", c.Param("transaction_id")).First(&original).Error; err != nil {
		return inventoryCorrectionResult{}, err
	}
	if original.Status != inventoryPosted || original.PostedAt == nil {
		return inventoryCorrectionResult{}, errors.New("only posted inventory transactions can be corrected")
	}
	var result inventoryCorrectionResult
	switch req.Kind {
	case "cost":
		result, err = s.costCorrection(original, req, actor.ID, key, fingerprint, persist)
	case "quantity":
		result, err = s.quantityCorrection(original, req, actor.ID, key, fingerprint, persist)
	default:
		result, err = s.voidCorrection(original, req.Reason, actor.ID, key, fingerprint, persist)
	}
	if err != nil && persist {
		var replay models.InventoryCorrection
		if replayErr := correctionPreloads(s.db).Where("created_by_id = ? AND idempotency_key = ?", actor.ID, key).First(&replay).Error; replayErr == nil {
			if replay.RequestFingerprint != fingerprint {
				return inventoryCorrectionResult{}, errInventoryConflict
			}
			normalizeCorrection(&replay)
			return correctionResultFromModel(replay), nil
		}
	}
	return result, err
}

func correctionFingerprint(req inventoryCorrectionInput) (string, error) {
	encoded, err := json.Marshal(req)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func correctionResultFromModel(value models.InventoryCorrection) inventoryCorrectionResult {
	strategy := value.Kind
	if strategy == "void" {
		strategy = "exact_reversal"
	} else if strategy == "cost" {
		strategy = "moving_average_recost"
	} else if strategy == "quantity" {
		strategy = "current_quantity_adjustment"
	}
	return inventoryCorrectionResult{Strategy: strategy, InventoryValueDeltaSGD: decimalOrZero(value.InventoryValueDeltaSGD), HistoricalOutflowCostDeltaSGD: decimalOrZero(value.HistoricalOutflowCostDeltaSGD), Correction: &value, Impacts: []inventoryCorrectionImpact{}}
}

func (s *Server) costCorrection(original models.InventoryTransaction, req inventoryCorrectionInput, actorID uint, key, fingerprint string, persist bool) (inventoryCorrectionResult, error) {
	if original.Type != "purchase_receipt" {
		return inventoryCorrectionResult{}, errors.New("cost corrections are allowed only for purchase receipts")
	}
	plan, err := s.buildCostCorrectionPlan(s.db, original, req)
	if err != nil {
		return inventoryCorrectionResult{}, err
	}
	resolvedBySKU := map[uint]models.InventoryTransactionLine{}
	for _, line := range plan.Prepared.Lines {
		resolvedBySKU[line.SKUID] = line
	}
	for index := range req.Lines {
		for _, originalLine := range original.Lines {
			if originalLine.SKU.PublicID == req.Lines[index].SKUID {
				resolved := resolvedBySKU[originalLine.SKUID]
				req.Lines[index].SourceCurrency = resolved.SourceCurrency
				req.Lines[index].SourceUnitPrice = resolved.SourceUnitPrice
				req.Lines[index].FXRateToSGD = resolved.FXRateToSGD
				break
			}
		}
	}
	for index := range req.Charges {
		if index < len(plan.Prepared.Charges) {
			req.Charges[index].SourceCurrency = plan.Prepared.Charges[index].SourceCurrency
			req.Charges[index].SourceAmount = plan.Prepared.Charges[index].SourceAmount
			req.Charges[index].FXRateToSGD = plan.Prepared.Charges[index].FXRateToSGD
		}
	}
	result := inventoryCorrectionResult{Strategy: "moving_average_recost", InventoryValueDeltaSGD: money.Format(plan.InventoryDelta), HistoricalOutflowCostDeltaSGD: money.Format(plan.OutflowDelta), Impacts: plan.Impacts}
	if !persist {
		return result, nil
	}
	err = s.db.Transaction(func(tx *gorm.DB) error {
		var locked models.InventoryTransaction
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&locked, original.ID).Error; err != nil {
			return err
		}
		lockedLines := append([]models.InventoryTransactionLine(nil), original.Lines...)
		sort.Slice(lockedLines, func(i, j int) bool { return lockedLines[i].SKUID < lockedLines[j].SKUID })
		for _, line := range lockedLines {
			var sku models.SKU
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&sku, line.SKUID).Error; err != nil {
				return err
			}
		}
		// Rebuild after locks so a concurrent posting cannot make the preview stale.
		fresh, err := s.buildCostCorrectionPlan(tx, original, req)
		if err != nil {
			return err
		}
		var count int64
		if err := tx.Model(&models.InventoryCorrection{}).Where("original_transaction_id = ?", original.ID).Count(&count).Error; err != nil {
			return err
		}
		correction := models.InventoryCorrection{OriginalTransactionID: original.ID, Kind: "cost", Reason: req.Reason, Version: int(count) + 1, IdempotencyKey: key, RequestFingerprint: fingerprint, CreatedByID: actorID, InventoryValueDeltaSGD: money.Format(fresh.InventoryDelta), HistoricalOutflowCostDeltaSGD: money.Format(fresh.OutflowDelta)}
		if err := tx.Create(&correction).Error; err != nil {
			return err
		}
		for i := range fresh.Lines {
			fresh.Lines[i].InventoryCorrectionID = correction.ID
			if err := tx.Create(&fresh.Lines[i]).Error; err != nil {
				return err
			}
		}
		for i := range fresh.Charges {
			fresh.Charges[i].InventoryCorrectionID = correction.ID
			if err := tx.Create(&fresh.Charges[i]).Error; err != nil {
				return err
			}
		}
		for i := range fresh.Revisions {
			fresh.Revisions[i].InventoryCorrectionID = correction.ID
			if err := tx.Create(&fresh.Revisions[i]).Error; err != nil {
				return err
			}
		}
		for skuID, ending := range fresh.EndingStates {
			updates := map[string]any{"average_unit_cost_sgd": ending.AverageCostAfterSGD, "inventory_value_sgd": ending.InventoryValueAfterSGD, "zero_cost_opening": money.Must(ending.AverageCostAfterSGD).Sign() == 0}
			if err := tx.Model(&models.SKU{}).Where("id = ?", skuID).Updates(updates).Error; err != nil {
				return err
			}
		}
		result.InventoryValueDeltaSGD = correction.InventoryValueDeltaSGD
		result.HistoricalOutflowCostDeltaSGD = correction.HistoricalOutflowCostDeltaSGD
		result.Impacts = fresh.Impacts
		result.Correction = &correction
		return nil
	})
	if err != nil {
		return inventoryCorrectionResult{}, err
	}
	if result.Correction != nil {
		var loaded models.InventoryCorrection
		if err := correctionPreloads(s.db).First(&loaded, result.Correction.ID).Error; err == nil {
			normalizeCorrection(&loaded)
			result.Correction = &loaded
		}
	}
	return result, nil
}

func (s *Server) buildCostCorrectionPlan(db *gorm.DB, original models.InventoryTransaction, req inventoryCorrectionInput) (costCorrectionPlan, error) {
	normalized, err := normalizeInventoryInput(inventoryTransactionInput{Type: "purchase_receipt", BusinessDate: original.BusinessDate.Format("2006-01-02"), Note: req.Reason, Lines: req.Lines, Charges: req.Charges})
	if err != nil {
		return costCorrectionPlan{}, err
	}
	if len(normalized.Lines) != len(original.Lines) {
		return costCorrectionPlan{}, errors.New("cost correction must include every original purchase line")
	}
	originalByPublicSKU := map[string]models.InventoryTransactionLine{}
	for _, line := range original.Lines {
		originalByPublicSKU[line.SKU.PublicID] = line
	}
	prepared := models.InventoryTransaction{ID: original.ID, Type: "purchase_receipt", BusinessDate: original.BusinessDate}
	correctionLines := make([]models.InventoryCorrectionLine, 0, len(normalized.Lines))
	seen := map[uint]bool{}
	for _, input := range normalized.Lines {
		source, ok := originalByPublicSKU[input.SKUID]
		if !ok || seen[source.SKUID] || input.Quantity != source.QuantityDelta {
			return costCorrectionPlan{}, errors.New("cost correction cannot change SKU or quantity")
		}
		seen[source.SKUID] = true
		line := models.InventoryTransactionLine{ID: source.ID, InventoryTransactionID: original.ID, SKUID: source.SKUID, QuantityDelta: source.QuantityDelta, SourceCurrency: input.SourceCurrency, SourceUnitPrice: input.SourceUnitPrice, FXRateToSGD: input.FXRateToSGD}
		prepared.Lines = append(prepared.Lines, line)
	}
	for _, input := range normalized.Charges {
		prepared.Charges = append(prepared.Charges, models.InventoryCharge{InventoryTransactionID: original.ID, Type: input.Type, SourceCurrency: input.SourceCurrency, SourceAmount: input.SourceAmount, FXRateToSGD: input.FXRateToSGD})
	}
	if err := s.prepareInventoryRates(nil, &prepared); err != nil {
		return costCorrectionPlan{}, err
	}
	values, err := calculateCorrectedPurchaseValues(prepared.Lines, prepared.Charges)
	if err != nil {
		return costCorrectionPlan{}, err
	}
	for _, line := range prepared.Lines {
		correctionLines = append(correctionLines, models.InventoryCorrectionLine{OriginalTransactionLineID: line.ID, SKUID: line.SKUID, QuantityDelta: line.QuantityDelta, SourceCurrency: line.SourceCurrency, SourceUnitPrice: line.SourceUnitPrice, FXRateToSGD: line.FXRateToSGD})
	}
	correctionCharges := make([]models.InventoryCorrectionCharge, 0, len(prepared.Charges))
	for index, charge := range prepared.Charges {
		correctionCharges = append(correctionCharges, models.InventoryCorrectionCharge{Ordinal: index, Type: charge.Type, SourceCurrency: charge.SourceCurrency, SourceAmount: charge.SourceAmount, FXRateToSGD: charge.FXRateToSGD})
	}
	plan := costCorrectionPlan{Prepared: prepared, Values: values, Lines: correctionLines, Charges: correctionCharges, InventoryDelta: new(big.Rat), OutflowDelta: new(big.Rat), EndingStates: map[uint]effectiveCostState{}}
	sorted := append([]models.InventoryTransactionLine(nil), original.Lines...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].SKUID < sorted[j].SKUID })
	for _, target := range sorted {
		impact, revisions, ending, inventoryDelta, outflowDelta, err := s.replaySKUCost(db, original, target, values[target.SKUID])
		if err != nil {
			return costCorrectionPlan{}, err
		}
		plan.Impacts = append(plan.Impacts, impact)
		plan.Revisions = append(plan.Revisions, revisions...)
		plan.EndingStates[target.SKUID] = ending
		plan.InventoryDelta.Add(plan.InventoryDelta, inventoryDelta)
		plan.OutflowDelta.Add(plan.OutflowDelta, outflowDelta)
	}
	return plan, nil
}

func calculateCorrectedPurchaseValues(lines []models.InventoryTransactionLine, charges []models.InventoryCharge) (map[uint]purchaseCostValue, error) {
	merchandise := map[uint]*big.Rat{}
	totalMerchandise, totalCharges := new(big.Rat), new(big.Rat)
	for _, line := range lines {
		amount := money.Mul(money.Int(abs(line.QuantityDelta)), money.Must(line.SourceUnitPrice), money.Must(line.FXRateToSGD))
		merchandise[line.ID] = amount
		totalMerchandise.Add(totalMerchandise, amount)
	}
	for _, charge := range charges {
		totalCharges.Add(totalCharges, money.Mul(money.Must(charge.SourceAmount), money.Must(charge.FXRateToSGD)))
	}
	if totalCharges.Sign() > 0 && totalMerchandise.Sign() == 0 {
		return nil, errors.New("charges cannot be allocated when merchandise value is zero")
	}
	sorted := append([]models.InventoryTransactionLine(nil), lines...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })
	allocated := new(big.Rat)
	values := map[uint]purchaseCostValue{}
	for index, line := range sorted {
		allocation := new(big.Rat)
		if totalCharges.Sign() > 0 {
			if index == len(sorted)-1 {
				allocation.Sub(totalCharges, allocated)
			} else {
				allocation.Mul(totalCharges, merchandise[line.ID])
				allocation.Quo(allocation, totalMerchandise)
				allocation = money.Must(money.Format(allocation))
				allocated.Add(allocated, allocation)
			}
		}
		movement := money.Add(merchandise[line.ID], allocation)
		values[line.SKUID] = purchaseCostValue{Merchandise: money.Format(merchandise[line.ID]), Allocated: money.Format(allocation), Movement: money.Format(movement), Landed: money.Format(new(big.Rat).Quo(movement, money.Int(line.QuantityDelta)))}
	}
	return values, nil
}

func effectiveStateForLine(db *gorm.DB, line models.InventoryTransactionLine) (effectiveCostState, error) {
	state := effectiveCostState{MerchandiseAmountSGD: decimalOrZero(line.MerchandiseAmountSGD), AllocatedChargesSGD: decimalOrZero(line.AllocatedChargesSGD), LandedUnitCostSGD: decimalOrZero(line.LandedUnitCostSGD), MovementCostSGD: decimalOrZero(line.MovementCostSGD), AverageCostBeforeSGD: decimalOrZero(line.AverageCostBeforeSGD), AverageCostAfterSGD: decimalOrZero(line.AverageCostAfterSGD), InventoryValueBeforeSGD: decimalOrZero(line.InventoryValueBeforeSGD), InventoryValueAfterSGD: decimalOrZero(line.InventoryValueAfterSGD)}
	var revision models.InventoryCostRevision
	err := db.Where("inventory_transaction_line_id = ?", line.ID).Order("id DESC").First(&revision).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	state.MerchandiseAmountSGD = revision.EffectiveMerchandiseAmountSGD
	state.AllocatedChargesSGD = revision.EffectiveAllocatedChargesSGD
	state.LandedUnitCostSGD = revision.EffectiveLandedUnitCostSGD
	state.MovementCostSGD = revision.EffectiveMovementCostSGD
	state.AverageCostBeforeSGD = revision.EffectiveAverageCostBeforeSGD
	state.AverageCostAfterSGD = revision.EffectiveAverageCostAfterSGD
	state.InventoryValueBeforeSGD = revision.EffectiveInventoryValueBeforeSGD
	state.InventoryValueAfterSGD = revision.EffectiveInventoryValueAfterSGD
	var correction models.InventoryCorrection
	if err := db.First(&correction, revision.InventoryCorrectionID).Error; err == nil {
		state.Version = correction.Version
	}
	return state, nil
}

func (s *Server) replaySKUCost(db *gorm.DB, original models.InventoryTransaction, target models.InventoryTransactionLine, corrected purchaseCostValue) (inventoryCorrectionImpact, []models.InventoryCostRevision, effectiveCostState, *big.Rat, *big.Rat, error) {
	initial, err := effectiveStateForLine(db, target)
	if err != nil {
		return inventoryCorrectionImpact{}, nil, effectiveCostState{}, nil, nil, err
	}
	quantity := target.QuantityBefore
	average := money.Must(initial.AverageCostBeforeSGD)
	value := money.Must(initial.InventoryValueBeforeSGD)
	var transactions []models.InventoryTransaction
	query := db.Preload("Lines").Where("status = ? AND (posted_at > ? OR (posted_at = ? AND id >= ?))", inventoryPosted, *original.PostedAt, *original.PostedAt, original.ID).Order("posted_at ASC, id ASC")
	if err := query.Find(&transactions).Error; err != nil {
		return inventoryCorrectionImpact{}, nil, effectiveCostState{}, nil, nil, err
	}
	var sku models.SKU
	if err := db.First(&sku, target.SKUID).Error; err != nil {
		return inventoryCorrectionImpact{}, nil, effectiveCostState{}, nil, nil, err
	}
	currentValue := money.Must(sku.InventoryValueSGD)
	currentAverage := money.Must(sku.AverageUnitCostSGD)
	outflowDelta := new(big.Rat)
	revisions := []models.InventoryCostRevision{}
	for _, transaction := range transactions {
		var line *models.InventoryTransactionLine
		for i := range transaction.Lines {
			if transaction.Lines[i].SKUID == target.SKUID {
				line = &transaction.Lines[i]
				break
			}
		}
		if line == nil {
			continue
		}
		old, err := effectiveStateForLine(db, *line)
		if err != nil {
			return inventoryCorrectionImpact{}, nil, effectiveCostState{}, nil, nil, err
		}
		beforeAverage := new(big.Rat).Set(average)
		beforeValue := new(big.Rat).Set(value)
		quantity += line.QuantityDelta
		if quantity < 0 {
			return inventoryCorrectionImpact{}, nil, effectiveCostState{}, nil, nil, errors.New("cost replay would produce negative stock")
		}
		movement := new(big.Rat)
		merchandise, allocation, landed := "0.00000000", "0.00000000", money.Format(beforeAverage)
		if transaction.ID == original.ID {
			movement.Set(money.Must(corrected.Movement))
			merchandise, allocation, landed = corrected.Merchandise, corrected.Allocated, corrected.Landed
			value.Add(value, movement)
			if quantity > 0 {
				average.Quo(value, money.Int(quantity))
			}
		} else if transaction.Type == "purchase_receipt" && line.QuantityDelta > 0 {
			movement.Set(money.Must(old.MovementCostSGD))
			merchandise, allocation, landed = old.MerchandiseAmountSGD, old.AllocatedChargesSGD, old.LandedUnitCostSGD
			value.Add(value, movement)
			if quantity > 0 {
				average.Quo(value, money.Int(quantity))
			}
		} else {
			movement.Mul(money.Int(abs(line.QuantityDelta)), beforeAverage)
			landed = money.Format(beforeAverage)
			if line.QuantityDelta > 0 {
				value.Add(value, movement)
				outflowDelta.Sub(outflowDelta, new(big.Rat).Sub(new(big.Rat).Set(movement), money.Must(old.MovementCostSGD)))
			} else {
				value.Sub(value, movement)
				outflowDelta.Add(outflowDelta, new(big.Rat).Sub(new(big.Rat).Set(movement), money.Must(old.MovementCostSGD)))
			}
		}
		if quantity == 0 && value.Sign() < 0 {
			value.SetInt64(0)
		}
		// Posting persists DECIMAL(20,8) snapshots and subsequent movements use
		// those frozen values, so replay must cross the same rounding boundary.
		value = money.Must(money.Format(value))
		average = money.Must(money.Format(average))
		revisions = append(revisions, models.InventoryCostRevision{InventoryTransactionLineID: line.ID, EffectiveMerchandiseAmountSGD: merchandise, EffectiveAllocatedChargesSGD: allocation, EffectiveLandedUnitCostSGD: landed, EffectiveMovementCostSGD: money.Format(movement), EffectiveAverageCostBeforeSGD: money.Format(beforeAverage), EffectiveAverageCostAfterSGD: money.Format(average), EffectiveInventoryValueBeforeSGD: money.Format(beforeValue), EffectiveInventoryValueAfterSGD: money.Format(value)})
	}
	if quantity != sku.Stock {
		return inventoryCorrectionImpact{}, nil, effectiveCostState{}, nil, nil, errors.New("inventory ledger quantity does not match SKU balance")
	}
	ending := effectiveCostState{AverageCostAfterSGD: money.Format(average), InventoryValueAfterSGD: money.Format(value)}
	inventoryDelta := new(big.Rat).Sub(new(big.Rat).Set(value), currentValue)
	impact := inventoryCorrectionImpact{SKUID: sku.PublicID, SKUCode: sku.Code, CurrentAverageCostSGD: money.Format(currentAverage), EffectiveAverageCostSGD: money.Format(average), CurrentInventoryValueSGD: money.Format(currentValue), EffectiveInventoryValueSGD: money.Format(value), InventoryValueDeltaSGD: money.Format(inventoryDelta), HistoricalOutflowCostDeltaSGD: money.Format(outflowDelta), AffectedTransactionCount: len(revisions)}
	return impact, revisions, ending, inventoryDelta, outflowDelta, nil
}

func (s *Server) voidCorrection(original models.InventoryTransaction, reason string, actorID uint, key, fingerprint string, persist bool) (inventoryCorrectionResult, error) {
	impacts, err := s.validateExactReversal(s.db, original)
	if err != nil {
		return inventoryCorrectionResult{}, err
	}
	total := new(big.Rat)
	for _, impact := range impacts {
		total.Add(total, money.Must(impact.InventoryValueDeltaSGD))
	}
	result := inventoryCorrectionResult{Strategy: "exact_reversal", InventoryValueDeltaSGD: money.Format(total), HistoricalOutflowCostDeltaSGD: "0.00000000", Impacts: impacts}
	if !persist {
		return result, nil
	}
	err = s.db.Transaction(func(tx *gorm.DB) error {
		var lockedOriginal models.InventoryTransaction
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&lockedOriginal, original.ID).Error; err != nil {
			return err
		}
		lockedLines := append([]models.InventoryTransactionLine(nil), original.Lines...)
		sort.Slice(lockedLines, func(i, j int) bool { return lockedLines[i].SKUID < lockedLines[j].SKUID })
		for _, line := range lockedLines {
			var sku models.SKU
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&sku, line.SKUID).Error; err != nil {
				return err
			}
		}
		if _, validateErr := s.validateExactReversal(tx, original); validateErr != nil {
			return validateErr
		}
		var count int64
		if err := tx.Model(&models.InventoryCorrection{}).Where("original_transaction_id = ?", original.ID).Count(&count).Error; err != nil {
			return err
		}
		now := time.Now().UTC()
		reversal := models.InventoryTransaction{Type: "stock_adjustment", Status: inventoryPosted, BusinessDate: now, Note: "Exact reversal of " + original.PublicID + ": " + reason, CreatedByID: actorID, PostedByID: &actorID, ReversalOfID: &original.ID, PostedAt: &now}
		if err := tx.Create(&reversal).Error; err != nil {
			return err
		}
		for _, source := range original.Lines {
			var sku models.SKU
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&sku, source.SKUID).Error; err != nil {
				return err
			}
			effective, err := effectiveStateForLine(tx, source)
			if err != nil {
				return err
			}
			line := models.InventoryTransactionLine{InventoryTransactionID: reversal.ID, SKUID: source.SKUID, QuantityDelta: -source.QuantityDelta, SourceCurrency: "SGD", SourceUnitPrice: "0.00000000", FXRateToSGD: "1.00000000", FXRateDate: &now, FXRateSource: "exact_reversal", MerchandiseAmountSGD: effective.MerchandiseAmountSGD, AllocatedChargesSGD: effective.AllocatedChargesSGD, LandedUnitCostSGD: effective.LandedUnitCostSGD, MovementCostSGD: effective.MovementCostSGD, QuantityBefore: source.QuantityAfter, QuantityAfter: source.QuantityBefore, AverageCostBeforeSGD: effective.AverageCostAfterSGD, AverageCostAfterSGD: effective.AverageCostBeforeSGD, InventoryValueBeforeSGD: effective.InventoryValueAfterSGD, InventoryValueAfterSGD: effective.InventoryValueBeforeSGD}
			if err := tx.Create(&line).Error; err != nil {
				return err
			}
			sku.Stock, sku.AverageUnitCostSGD, sku.InventoryValueSGD = source.QuantityBefore, effective.AverageCostBeforeSGD, effective.InventoryValueBeforeSGD
			sku.ZeroCostOpening = money.Must(sku.AverageUnitCostSGD).Sign() == 0
			if err := tx.Save(&sku).Error; err != nil {
				return err
			}
		}
		correction := models.InventoryCorrection{OriginalTransactionID: original.ID, Kind: "void", Reason: reason, Version: int(count) + 1, IdempotencyKey: key, RequestFingerprint: fingerprint, CreatedByID: actorID, ResultTransactionID: &reversal.ID, InventoryValueDeltaSGD: money.Format(total), HistoricalOutflowCostDeltaSGD: "0.00000000"}
		if err := tx.Create(&correction).Error; err != nil {
			return err
		}
		result.Correction = &correction
		return nil
	})
	if err != nil {
		return inventoryCorrectionResult{}, err
	}
	if result.Correction != nil {
		var loaded models.InventoryCorrection
		if loadErr := correctionPreloads(s.db).First(&loaded, result.Correction.ID).Error; loadErr == nil {
			normalizeCorrection(&loaded)
			result.Correction = &loaded
		}
	}
	return result, nil
}

func (s *Server) validateExactReversal(db *gorm.DB, original models.InventoryTransaction) ([]inventoryCorrectionImpact, error) {
	var existing models.InventoryTransaction
	if err := db.Where("reversal_of_id = ?", original.ID).First(&existing).Error; err == nil {
		return nil, errors.New("inventory transaction has already been reversed")
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	impacts := []inventoryCorrectionImpact{}
	for _, line := range original.Lines {
		var latest models.InventoryTransactionLine
		err := db.Joins("JOIN inventory_transactions ON inventory_transactions.id = inventory_transaction_lines.inventory_transaction_id").Where("inventory_transaction_lines.sk_uid = ? AND inventory_transactions.status = ?", line.SKUID, inventoryPosted).Order("inventory_transactions.posted_at DESC, inventory_transactions.id DESC, inventory_transaction_lines.id DESC").First(&latest).Error
		if err != nil {
			return nil, err
		}
		var sku models.SKU
		if err := db.First(&sku, line.SKUID).Error; err != nil {
			return nil, err
		}
		effective, err := effectiveStateForLine(db, line)
		if err != nil {
			return nil, err
		}
		if latest.ID != line.ID || sku.Stock != line.QuantityAfter || money.Format(money.Must(sku.AverageUnitCostSGD)) != money.Format(money.Must(effective.AverageCostAfterSGD)) || money.Format(money.Must(sku.InventoryValueSGD)) != money.Format(money.Must(effective.InventoryValueAfterSGD)) {
			return nil, errors.New("exact reversal is unsafe because a related SKU has later inventory activity; use cost or quantity correction")
		}
		impacts = append(impacts, inventoryCorrectionImpact{SKUID: sku.PublicID, SKUCode: sku.Code, CurrentAverageCostSGD: decimalOrZero(sku.AverageUnitCostSGD), EffectiveAverageCostSGD: decimalOrZero(effective.AverageCostBeforeSGD), CurrentInventoryValueSGD: decimalOrZero(sku.InventoryValueSGD), EffectiveInventoryValueSGD: decimalOrZero(effective.InventoryValueBeforeSGD), InventoryValueDeltaSGD: money.Format(new(big.Rat).Sub(money.Must(effective.InventoryValueBeforeSGD), money.Must(effective.InventoryValueAfterSGD))), HistoricalOutflowCostDeltaSGD: "0.00000000", AffectedTransactionCount: 1})
	}
	return impacts, nil
}

func (s *Server) quantityCorrection(original models.InventoryTransaction, req inventoryCorrectionInput, actorID uint, key, fingerprint string, persist bool) (inventoryCorrectionResult, error) {
	if len(req.Lines) != len(original.Lines) || len(req.Charges) != 0 {
		return inventoryCorrectionResult{}, errors.New("quantity correction must include every original line and no charges")
	}
	originalBySKU := map[string]models.InventoryTransactionLine{}
	for _, line := range original.Lines {
		originalBySKU[line.SKU.PublicID] = line
	}
	type deltaLine struct {
		source models.InventoryTransactionLine
		delta  int
	}
	deltas := []deltaLine{}
	for _, input := range req.Lines {
		source, ok := originalBySKU[input.SKUID]
		if !ok || input.Quantity == 0 {
			return inventoryCorrectionResult{}, errors.New("quantity correction cannot change SKU and requires non-zero corrected quantities")
		}
		corrected := normalizedQuantity(original.Type, input.Quantity)
		deltas = append(deltas, deltaLine{source: source, delta: corrected - source.QuantityDelta})
	}
	sort.Slice(deltas, func(i, j int) bool { return deltas[i].source.SKUID < deltas[j].source.SKUID })
	hasChange := false
	for _, item := range deltas {
		hasChange = hasChange || item.delta != 0
	}
	if !hasChange {
		return inventoryCorrectionResult{}, errors.New("quantity correction does not change any quantity")
	}
	impacts := []inventoryCorrectionImpact{}
	total := new(big.Rat)
	for _, item := range deltas {
		var sku models.SKU
		if err := s.db.First(&sku, item.source.SKUID).Error; err != nil {
			return inventoryCorrectionResult{}, err
		}
		if sku.Stock+item.delta < 0 {
			return inventoryCorrectionResult{}, errors.New("quantity correction would produce negative stock")
		}
		deltaValue := money.Mul(money.Int(item.delta), money.Must(sku.AverageUnitCostSGD))
		total.Add(total, deltaValue)
		impacts = append(impacts, inventoryCorrectionImpact{SKUID: sku.PublicID, SKUCode: sku.Code, CurrentAverageCostSGD: decimalOrZero(sku.AverageUnitCostSGD), EffectiveAverageCostSGD: decimalOrZero(sku.AverageUnitCostSGD), CurrentInventoryValueSGD: decimalOrZero(sku.InventoryValueSGD), EffectiveInventoryValueSGD: money.Format(new(big.Rat).Add(money.Must(sku.InventoryValueSGD), deltaValue)), InventoryValueDeltaSGD: money.Format(deltaValue), HistoricalOutflowCostDeltaSGD: "0.00000000", AffectedTransactionCount: 1})
	}
	result := inventoryCorrectionResult{Strategy: "current_quantity_adjustment", InventoryValueDeltaSGD: money.Format(total), HistoricalOutflowCostDeltaSGD: "0.00000000", Impacts: impacts}
	if !persist {
		return result, nil
	}
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var lockedOriginal models.InventoryTransaction
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&lockedOriginal, original.ID).Error; err != nil {
			return err
		}
		var count int64
		if err := tx.Model(&models.InventoryCorrection{}).Where("original_transaction_id = ?", original.ID).Count(&count).Error; err != nil {
			return err
		}
		now := time.Now().UTC()
		adjustment := models.InventoryTransaction{Type: "stock_adjustment", Status: inventoryPosted, BusinessDate: now, Note: "Quantity correction of " + original.PublicID + ": " + req.Reason, CreatedByID: actorID, PostedByID: &actorID, PostedAt: &now}
		if err := tx.Create(&adjustment).Error; err != nil {
			return err
		}
		txTotal := new(big.Rat)
		for _, item := range deltas {
			if item.delta == 0 {
				continue
			}
			var sku models.SKU
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&sku, item.source.SKUID).Error; err != nil {
				return err
			}
			if sku.Stock+item.delta < 0 {
				return errors.New("quantity correction would produce negative stock")
			}
			beforeValue, average := money.Must(sku.InventoryValueSGD), money.Must(sku.AverageUnitCostSGD)
			movement := money.Mul(money.Int(abs(item.delta)), average)
			signedDelta := new(big.Rat).Set(movement)
			if item.delta < 0 {
				signedDelta.Neg(signedDelta)
			}
			txTotal.Add(txTotal, signedDelta)
			afterValue := new(big.Rat).Set(beforeValue)
			if item.delta > 0 {
				afterValue.Add(afterValue, movement)
			} else {
				afterValue.Sub(afterValue, movement)
			}
			line := models.InventoryTransactionLine{InventoryTransactionID: adjustment.ID, SKUID: sku.ID, QuantityDelta: item.delta, SourceCurrency: "SGD", SourceUnitPrice: "0.00000000", FXRateToSGD: "1.00000000", FXRateDate: &now, FXRateSource: "quantity_correction", LandedUnitCostSGD: money.Format(average), MovementCostSGD: money.Format(movement), QuantityBefore: sku.Stock, QuantityAfter: sku.Stock + item.delta, AverageCostBeforeSGD: money.Format(average), AverageCostAfterSGD: money.Format(average), InventoryValueBeforeSGD: money.Format(beforeValue), InventoryValueAfterSGD: money.Format(afterValue)}
			if err := tx.Create(&line).Error; err != nil {
				return err
			}
			sku.Stock, sku.InventoryValueSGD = line.QuantityAfter, line.InventoryValueAfterSGD
			if err := tx.Save(&sku).Error; err != nil {
				return err
			}
		}
		correction := models.InventoryCorrection{OriginalTransactionID: original.ID, Kind: "quantity", Reason: req.Reason, Version: int(count) + 1, IdempotencyKey: key, RequestFingerprint: fingerprint, CreatedByID: actorID, ResultTransactionID: &adjustment.ID, InventoryValueDeltaSGD: money.Format(txTotal), HistoricalOutflowCostDeltaSGD: "0.00000000"}
		if err := tx.Create(&correction).Error; err != nil {
			return err
		}
		result.Correction = &correction
		result.InventoryValueDeltaSGD = correction.InventoryValueDeltaSGD
		return nil
	})
	if err != nil {
		return inventoryCorrectionResult{}, err
	}
	if result.Correction != nil {
		var loaded models.InventoryCorrection
		if loadErr := correctionPreloads(s.db).First(&loaded, result.Correction.ID).Error; loadErr == nil {
			normalizeCorrection(&loaded)
			result.Correction = &loaded
		}
	}
	return result, nil
}
