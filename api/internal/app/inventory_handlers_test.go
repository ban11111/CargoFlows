package app

import (
	"math/big"
	"strings"
	"testing"
	"time"

	"cargoflows/api/internal/models"
	"cargoflows/api/internal/money"
)

func TestInventoryPostingAllocatesChargesAndMaintainsMovingAverage(t *testing.T) {
	db := newTestDB(t)
	user := models.User{Name: "Cost operator", Email: "cost@example.test", PasswordHash: "x", Role: models.RoleAdmin, Status: "active"}
	category := models.Category{Name: "Inventory"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&category).Error; err != nil {
		t.Fatal(err)
	}
	product := models.Product{Name: "Cases", CategoryID: category.ID}
	if err := db.Create(&product).Error; err != nil {
		t.Fatal(err)
	}
	skus := []models.SKU{{ProductID: product.ID, Code: "COST-A", Status: "active", AverageUnitCostSGD: "0", InventoryValueSGD: "0"}, {ProductID: product.ID, Code: "COST-B", Status: "active", AverageUnitCostSGD: "0", InventoryValueSGD: "0"}}
	for i := range skus {
		if err := db.Create(&skus[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	date := time.Date(2026, 7, 18, 0, 0, 0, 0, time.UTC)
	receipt := models.InventoryTransaction{Type: "purchase_receipt", Status: inventoryDraft, BusinessDate: date, CreatedByID: user.ID}
	if err := db.Create(&receipt).Error; err != nil {
		t.Fatal(err)
	}
	receipt.Lines = []models.InventoryTransactionLine{
		{InventoryTransactionID: receipt.ID, SKUID: skus[0].ID, QuantityDelta: 10, SourceCurrency: "CNY", SourceUnitPrice: "100.00000000", FXRateToSGD: "0.20000000", FXRateDate: &date, FXRateSource: "manual"},
		{InventoryTransactionID: receipt.ID, SKUID: skus[1].ID, QuantityDelta: 10, SourceCurrency: "CNY", SourceUnitPrice: "50.00000000", FXRateToSGD: "0.20000000", FXRateDate: &date, FXRateSource: "manual"},
	}
	for i := range receipt.Lines {
		if err := db.Create(&receipt.Lines[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	receipt.Charges = []models.InventoryCharge{{InventoryTransactionID: receipt.ID, Type: "freight", SourceCurrency: "CNY", SourceAmount: "30.00000000", FXRateToSGD: "0.20000000", FXRateDate: &date, FXRateSource: "manual"}}
	if err := db.Create(&receipt.Charges[0]).Error; err != nil {
		t.Fatal(err)
	}
	server := &Server{db: db}
	if err := server.postPreparedInventory(&receipt, user.ID); err != nil {
		t.Fatal(err)
	}

	var lines []models.InventoryTransactionLine
	if err := db.Where("inventory_transaction_id = ?", receipt.ID).Order("id").Find(&lines).Error; err != nil {
		t.Fatal(err)
	}
	if money.Format(money.Must(lines[0].AllocatedChargesSGD)) != "4.00000000" || money.Format(money.Must(lines[1].AllocatedChargesSGD)) != "2.00000000" {
		t.Fatalf("allocations=%s,%s", lines[0].AllocatedChargesSGD, lines[1].AllocatedChargesSGD)
	}
	if money.Format(money.Must(lines[0].LandedUnitCostSGD)) != "20.40000000" || money.Format(money.Must(lines[1].LandedUnitCostSGD)) != "10.20000000" {
		t.Fatalf("landed costs=%s,%s", lines[0].LandedUnitCostSGD, lines[1].LandedUnitCostSGD)
	}
	if err := db.First(&skus[0], skus[0].ID).Error; err != nil {
		t.Fatal(err)
	}
	if skus[0].Stock != 10 || money.Format(money.Must(skus[0].AverageUnitCostSGD)) != "20.40000000" || money.Format(money.Must(skus[0].InventoryValueSGD)) != "204.00000000" {
		t.Fatalf("sku A=%+v", skus[0])
	}

	issue := models.InventoryTransaction{Type: "sale_issue", Status: inventoryDraft, BusinessDate: date, CreatedByID: user.ID}
	if err := db.Create(&issue).Error; err != nil {
		t.Fatal(err)
	}
	issue.Lines = []models.InventoryTransactionLine{{InventoryTransactionID: issue.ID, SKUID: skus[0].ID, QuantityDelta: -3, SourceCurrency: "SGD", SourceUnitPrice: "0", FXRateToSGD: "1", FXRateDate: &date, FXRateSource: "cost_ledger"}}
	if err := db.Create(&issue.Lines[0]).Error; err != nil {
		t.Fatal(err)
	}
	if err := server.postPreparedInventory(&issue, user.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&skus[0], skus[0].ID).Error; err != nil {
		t.Fatal(err)
	}
	if skus[0].Stock != 7 || money.Format(money.Must(skus[0].AverageUnitCostSGD)) != "20.40000000" || money.Format(money.Must(skus[0].InventoryValueSGD)) != "142.80000000" {
		t.Fatalf("sku after issue=%+v", skus[0])
	}
}

func TestECBRatesUseMostRecentPublishedWorkday(t *testing.T) {
	xml := `<Envelope><Cube><Cube time="2026-07-17"><Cube currency="CNY" rate="8.0"/><Cube currency="SGD" rate="1.6"/></Cube><Cube time="2026-07-16"><Cube currency="CNY" rate="7.9"/><Cube currency="SGD" rate="1.59"/></Cube></Cube></Envelope>`
	rates, date, err := parseECBRates(strings.NewReader(xml), time.Date(2026, 7, 19, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if date.Format("2006-01-02") != "2026-07-17" {
		t.Fatalf("date=%s", date)
	}
	if got := money.Format(new(big.Rat).Quo(rates["SGD"], rates["CNY"])); got != "0.20000000" {
		t.Fatalf("rate=%s", got)
	}
}

func TestInventoryPostingRejectsNegativeStockAndLeavesDraft(t *testing.T) {
	db := newTestDB(t)
	user := models.User{Name: "Operator", Email: "negative@example.test", PasswordHash: "x", Role: models.RoleOperator, Status: "active"}
	category := models.Category{Name: "Negative"}
	db.Create(&user)
	db.Create(&category)
	product := models.Product{Name: "Empty", CategoryID: category.ID}
	db.Create(&product)
	sku := models.SKU{ProductID: product.ID, Code: "EMPTY", Status: "active", AverageUnitCostSGD: "0", InventoryValueSGD: "0"}
	db.Create(&sku)
	date := time.Now().UTC()
	transaction := models.InventoryTransaction{Type: "sale_issue", Status: inventoryDraft, BusinessDate: date, CreatedByID: user.ID}
	db.Create(&transaction)
	line := models.InventoryTransactionLine{InventoryTransactionID: transaction.ID, SKUID: sku.ID, QuantityDelta: -1, SourceCurrency: "SGD", SourceUnitPrice: "0", FXRateToSGD: "1", FXRateDate: &date, FXRateSource: "cost_ledger"}
	db.Create(&line)
	transaction.Lines = []models.InventoryTransactionLine{line}
	if err := (&Server{db: db}).postPreparedInventory(&transaction, user.ID); err == nil {
		t.Fatal("expected negative stock rejection")
	}
	if err := db.First(&transaction, transaction.ID).Error; err != nil {
		t.Fatal(err)
	}
	if transaction.Status != inventoryDraft {
		t.Fatalf("status=%s", transaction.Status)
	}
}

func TestInventoryCostCorrectionReplaysMovingAverageWithoutMutatingOriginal(t *testing.T) {
	db := newTestDB(t)
	user := models.User{Name: "Recost operator", Email: "recost@example.test", PasswordHash: "x", Role: models.RoleAdmin, Status: "active"}
	category := models.Category{Name: "Recost"}
	db.Create(&user)
	db.Create(&category)
	product := models.Product{Name: "Recost cases", CategoryID: category.ID}
	db.Create(&product)
	sku := models.SKU{ProductID: product.ID, Code: "RECOST", Status: "active", AverageUnitCostSGD: "0", InventoryValueSGD: "0"}
	db.Create(&sku)
	server := &Server{db: db}
	date := time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)

	postReceipt := func(price string) models.InventoryTransaction {
		t.Helper()
		row := models.InventoryTransaction{Type: "purchase_receipt", Status: inventoryDraft, BusinessDate: date, CreatedByID: user.ID}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
		line := models.InventoryTransactionLine{InventoryTransactionID: row.ID, SKUID: sku.ID, QuantityDelta: 10, SourceCurrency: "SGD", SourceUnitPrice: price, FXRateToSGD: "1", FXRateDate: &date, FXRateSource: "manual"}
		if err := db.Create(&line).Error; err != nil {
			t.Fatal(err)
		}
		row.Lines = []models.InventoryTransactionLine{line}
		if err := server.postPreparedInventory(&row, user.ID); err != nil {
			t.Fatal(err)
		}
		return row
	}
	first := postReceipt("8")
	postReceipt("12")
	issue := models.InventoryTransaction{Type: "sale_issue", Status: inventoryDraft, BusinessDate: date, CreatedByID: user.ID}
	db.Create(&issue)
	issueLine := models.InventoryTransactionLine{InventoryTransactionID: issue.ID, SKUID: sku.ID, QuantityDelta: -10, SourceCurrency: "SGD", SourceUnitPrice: "0", FXRateToSGD: "1", FXRateDate: &date, FXRateSource: "cost_ledger"}
	db.Create(&issueLine)
	issue.Lines = []models.InventoryTransactionLine{issueLine}
	if err := server.postPreparedInventory(&issue, user.ID); err != nil {
		t.Fatal(err)
	}

	var original models.InventoryTransaction
	if err := db.Preload("Lines.SKU.Product").Preload("Charges").First(&original, first.ID).Error; err != nil {
		t.Fatal(err)
	}
	originalPrice := original.Lines[0].SourceUnitPrice
	result, err := server.costCorrection(original, inventoryCorrectionInput{Kind: "cost", Reason: "correct invoice", Lines: []inventoryLineInput{{SKUID: sku.PublicID, Quantity: 10, SourceCurrency: "SGD", SourceUnitPrice: "10", FXRateToSGD: "1"}}}, user.ID, "recost-1", "test-fingerprint", true)
	if err != nil {
		t.Fatal(err)
	}
	if result.InventoryValueDeltaSGD != "10.00000000" || result.HistoricalOutflowCostDeltaSGD != "10.00000000" {
		t.Fatalf("unexpected deltas: %+v", result)
	}
	if err := db.First(&sku, sku.ID).Error; err != nil {
		t.Fatal(err)
	}
	if money.Format(money.Must(sku.AverageUnitCostSGD)) != "11.00000000" || money.Format(money.Must(sku.InventoryValueSGD)) != "110.00000000" {
		t.Fatalf("corrected SKU=%+v", sku)
	}
	var unchanged models.InventoryTransactionLine
	if err := db.First(&unchanged, original.Lines[0].ID).Error; err != nil {
		t.Fatal(err)
	}
	if unchanged.SourceUnitPrice != originalPrice || money.Format(money.Must(unchanged.MovementCostSGD)) != "80.00000000" {
		t.Fatalf("original line was mutated: %+v", unchanged)
	}
	var revisionCount int64
	if err := db.Model(&models.InventoryCostRevision{}).Count(&revisionCount).Error; err != nil {
		t.Fatal(err)
	}
	if revisionCount != 3 {
		t.Fatalf("revision count=%d", revisionCount)
	}
}

func TestExactReversalRestoresStateAndRejectsLaterActivity(t *testing.T) {
	db := newTestDB(t)
	user := models.User{Name: "Reverse operator", Email: "reverse@example.test", PasswordHash: "x", Role: models.RoleAdmin, Status: "active"}
	category := models.Category{Name: "Reverse"}
	db.Create(&user)
	db.Create(&category)
	product := models.Product{Name: "Reverse product", CategoryID: category.ID}
	db.Create(&product)
	sku := models.SKU{ProductID: product.ID, Code: "REVERSE", Status: "active", AverageUnitCostSGD: "0", InventoryValueSGD: "0"}
	db.Create(&sku)
	server := &Server{db: db}
	date := time.Now().UTC()
	receipt := models.InventoryTransaction{Type: "purchase_receipt", Status: inventoryDraft, BusinessDate: date, CreatedByID: user.ID}
	db.Create(&receipt)
	line := models.InventoryTransactionLine{InventoryTransactionID: receipt.ID, SKUID: sku.ID, QuantityDelta: 5, SourceCurrency: "SGD", SourceUnitPrice: "4", FXRateToSGD: "1", FXRateDate: &date, FXRateSource: "manual"}
	db.Create(&line)
	receipt.Lines = []models.InventoryTransactionLine{line}
	if err := server.postPreparedInventory(&receipt, user.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.Preload("Lines.SKU.Product").First(&receipt, receipt.ID).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := server.voidCorrection(receipt, "wrong receipt", user.ID, "reverse-1", "test-fingerprint-1", true); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&sku, sku.ID).Error; err != nil {
		t.Fatal(err)
	}
	if sku.Stock != 0 || money.Must(sku.InventoryValueSGD).Sign() != 0 {
		t.Fatalf("state not restored: %+v", sku)
	}

	second := models.InventoryTransaction{Type: "purchase_receipt", Status: inventoryDraft, BusinessDate: date, CreatedByID: user.ID}
	db.Create(&second)
	secondLine := models.InventoryTransactionLine{InventoryTransactionID: second.ID, SKUID: sku.ID, QuantityDelta: 5, SourceCurrency: "SGD", SourceUnitPrice: "4", FXRateToSGD: "1", FXRateDate: &date, FXRateSource: "manual"}
	db.Create(&secondLine)
	second.Lines = []models.InventoryTransactionLine{secondLine}
	if err := server.postPreparedInventory(&second, user.ID); err != nil {
		t.Fatal(err)
	}
	issue := models.InventoryTransaction{Type: "sale_issue", Status: inventoryDraft, BusinessDate: date, CreatedByID: user.ID}
	db.Create(&issue)
	issueLine := models.InventoryTransactionLine{InventoryTransactionID: issue.ID, SKUID: sku.ID, QuantityDelta: -1, SourceCurrency: "SGD", SourceUnitPrice: "0", FXRateToSGD: "1", FXRateDate: &date, FXRateSource: "cost_ledger"}
	db.Create(&issueLine)
	issue.Lines = []models.InventoryTransactionLine{issueLine}
	if err := server.postPreparedInventory(&issue, user.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.Preload("Lines.SKU.Product").First(&second, second.ID).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := server.voidCorrection(second, "unsafe", user.ID, "reverse-2", "test-fingerprint-2", false); err == nil || !strings.Contains(err.Error(), "later inventory activity") {
		t.Fatalf("expected unsafe reversal rejection, got %v", err)
	}
}

func TestCorrectedPurchaseReallocatesChargesAcrossEverySKU(t *testing.T) {
	lines := []models.InventoryTransactionLine{
		{ID: 1, SKUID: 10, QuantityDelta: 10, SourceUnitPrice: "50", FXRateToSGD: "0.2"},
		{ID: 2, SKUID: 20, QuantityDelta: 10, SourceUnitPrice: "50", FXRateToSGD: "0.2"},
	}
	charges := []models.InventoryCharge{{SourceAmount: "30", FXRateToSGD: "0.2"}}
	values, err := calculateCorrectedPurchaseValues(lines, charges)
	if err != nil {
		t.Fatal(err)
	}
	if values[10].Allocated != "3.00000000" || values[20].Allocated != "3.00000000" {
		t.Fatalf("allocations=%+v", values)
	}
	if values[10].Movement != "103.00000000" || values[20].Movement != "103.00000000" {
		t.Fatalf("movements=%+v", values)
	}
	allocated := money.Add(money.Must(values[10].Allocated), money.Must(values[20].Allocated))
	if money.Format(allocated) != "6.00000000" {
		t.Fatalf("allocated total=%s", money.Format(allocated))
	}
}

func TestCorrectionFingerprintChangesWithRequestData(t *testing.T) {
	base := inventoryCorrectionInput{Kind: "cost", Reason: "invoice", Lines: []inventoryLineInput{{SKUID: "11111111-1111-4111-8111-111111111111", Quantity: 1, SourceCurrency: "SGD", SourceUnitPrice: "10", FXRateToSGD: "1"}}}
	first, err := correctionFingerprint(base)
	if err != nil {
		t.Fatal(err)
	}
	second, err := correctionFingerprint(base)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("same request produced different fingerprint")
	}
	base.Lines[0].SourceUnitPrice = "11"
	changed, err := correctionFingerprint(base)
	if err != nil {
		t.Fatal(err)
	}
	if changed == first {
		t.Fatal("different correction data reused fingerprint")
	}
}
