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
