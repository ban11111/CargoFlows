package sop

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"cargoflows/api/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func modelFamilyTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.User{}, &models.Product{}, &models.SKU{}, &models.ModelFamily{}, &models.ModelFamilyMember{}, &models.AIAuditEvent{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func seedModelFamilySKU(t *testing.T, db *gorm.DB, code string) models.SKU {
	t.Helper()
	product := models.Product{Name: code + " product", Category: "test"}
	if err := db.Create(&product).Error; err != nil {
		t.Fatal(err)
	}
	sku := models.SKU{ProductID: product.ID, Code: code}
	if err := db.Create(&sku).Error; err != nil {
		t.Fatal(err)
	}
	return sku
}

func createModelFamily(t *testing.T, service *ModelFamilyService, code string, actor uint) models.ModelFamily {
	t.Helper()
	family, err := service.Create(t.Context(), CreateModelFamilyInput{Brand: "CargoFlows", NameZH: "同款产品", NameEN: "Same model", ModelCode: code, CommonStructure: []byte(`{"schema":"model_family_common_structure_v1","invariants":["housing"]}`), VariationDimensions: []string{"color", "ports"}, CreatedByID: actor})
	if err != nil {
		t.Fatal(err)
	}
	return *family
}

func TestModelFamilyServiceRejectsSecondActiveFamilyAndKeepsRemovalHistory(t *testing.T) {
	db := modelFamilyTestDB(t)
	service := NewModelFamilyService(db)
	sku := seedModelFamilySKU(t, db, "MF-SKU-1")
	familyA := createModelFamily(t, service, "MODEL-A", 11)
	familyB := createModelFamily(t, service, "MODEL-B", 11)

	member, err := service.AddMember(t.Context(), familyA.PublicID, sku.PublicID, 12)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.AddMember(t.Context(), familyB.PublicID, sku.PublicID, 12); !errors.Is(err, ErrSKUAlreadyInModelFamily) {
		t.Fatalf("error = %v", err)
	}
	if err := service.RemoveMember(t.Context(), familyA.PublicID, member.PublicID, 13); err != nil {
		t.Fatal(err)
	}
	second, err := service.AddMember(t.Context(), familyB.PublicID, sku.PublicID, 14)
	if err != nil {
		t.Fatal(err)
	}
	if second.PublicID == member.PublicID {
		t.Fatal("re-add must create a new membership record")
	}
	var members []models.ModelFamilyMember
	if err := db.Order("created_at ASC").Find(&members).Error; err != nil {
		t.Fatal(err)
	}
	if len(members) != 2 || members[0].RemovedAt == nil || members[1].RemovedAt != nil {
		t.Fatalf("history = %#v", members)
	}
	var events []models.AIAuditEvent
	if err := db.Find(&events).Error; err != nil {
		t.Fatal(err)
	}
	if len(events) != 5 {
		t.Fatalf("audit events = %#v", events)
	}
}

func TestModelFamilyServiceAllowsSKUsFromDifferentProductsAndRejectsDuplicateModelCode(t *testing.T) {
	db := modelFamilyTestDB(t)
	service := NewModelFamilyService(db)
	family := createModelFamily(t, service, "MODEL-CROSS-PRODUCT", 1)
	first := seedModelFamilySKU(t, db, "MF-CROSS-ONE")
	second := seedModelFamilySKU(t, db, "MF-CROSS-TWO")
	if _, err := service.AddMember(t.Context(), family.PublicID, first.PublicID, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := service.AddMember(t.Context(), family.PublicID, second.PublicID, 1); err != nil {
		t.Fatalf("cross-product membership failed: %v", err)
	}
	_, err := service.Create(t.Context(), CreateModelFamilyInput{Brand: "CargoFlows", NameZH: "重复型号", NameEN: "Duplicate model", ModelCode: family.ModelCode, CommonStructure: []byte(`{"schema":"model_family_common_structure_v1","invariants":["housing"]}`), VariationDimensions: []string{"color"}, CreatedByID: 1})
	if !errors.Is(err, ErrModelCodeTaken) {
		t.Fatalf("duplicate model code error = %v", err)
	}
}

func TestModelFamilyServiceValidatesSchemaAndArchivedMutation(t *testing.T) {
	db := modelFamilyTestDB(t)
	service := NewModelFamilyService(db)
	_, err := service.Create(t.Context(), CreateModelFamilyInput{Brand: "CargoFlows", NameZH: "同款", NameEN: "Same", ModelCode: "BAD", CommonStructure: []byte(`{"schema":"wrong"}`), VariationDimensions: []string{"unknown"}, CreatedByID: 1})
	if !errors.Is(err, ErrModelFamilyInvalid) {
		t.Fatalf("invalid input error = %v", err)
	}
	_, err = service.Create(t.Context(), CreateModelFamilyInput{Brand: "CargoFlows", NameZH: "同款", NameEN: "Same", ModelCode: "BAD-UNKNOWN", CommonStructure: []byte(`{"schema":"model_family_common_structure_v1","invariants":["housing"],"untrusted":true}`), VariationDimensions: []string{"color"}, CreatedByID: 1})
	if !errors.Is(err, ErrModelFamilyInvalid) {
		t.Fatalf("unknown common-structure field error = %v", err)
	}
	family := createModelFamily(t, service, "MODEL-ARCHIVED", 1)
	sku := seedModelFamilySKU(t, db, "MF-SKU-2")
	archived := models.ModelFamilyArchived
	if _, err := service.Update(t.Context(), family.PublicID, UpdateModelFamilyInput{Status: &archived, UpdatedByID: 1}); err != nil {
		t.Fatal(err)
	}
	var auditsBefore int64
	if err := db.Model(&models.AIAuditEvent{}).Count(&auditsBefore).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := service.Update(t.Context(), family.PublicID, UpdateModelFamilyInput{Status: &archived, UpdatedByID: 1}); !errors.Is(err, ErrModelFamilyArchived) {
		t.Fatalf("repeat archive error = %v", err)
	}
	var auditsAfter int64
	if err := db.Model(&models.AIAuditEvent{}).Count(&auditsAfter).Error; err != nil {
		t.Fatal(err)
	}
	if auditsAfter != auditsBefore {
		t.Fatalf("repeat archive wrote audit: before=%d after=%d", auditsBefore, auditsAfter)
	}
	if _, err := service.AddMember(t.Context(), family.PublicID, sku.PublicID, 1); !errors.Is(err, ErrModelFamilyArchived) {
		t.Fatalf("error = %v", err)
	}
}

func TestModelFamilyServiceRejectsBlankMutableFields(t *testing.T) {
	db := modelFamilyTestDB(t)
	service := NewModelFamilyService(db)
	family := createModelFamily(t, service, "MODEL-BLANK-UPDATE", 1)
	blank := "   "
	if _, err := service.Update(t.Context(), family.PublicID, UpdateModelFamilyInput{NameEN: &blank, UpdatedByID: 1}); !errors.Is(err, ErrModelFamilyInvalid) {
		t.Fatalf("blank update error = %v", err)
	}
}

func TestModelFamilyServiceConcurrentAddHasOneActiveMember(t *testing.T) {
	db := modelFamilyTestDB(t)
	service := NewModelFamilyService(db)
	sku := seedModelFamilySKU(t, db, "MF-SKU-CONCURRENT")
	familyA := createModelFamily(t, service, "MODEL-CONCURRENT-A", 1)
	familyB := createModelFamily(t, service, "MODEL-CONCURRENT-B", 1)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, family := range []models.ModelFamily{familyA, familyB} {
		wg.Add(1)
		go func(f models.ModelFamily) {
			defer wg.Done()
			_, err := service.AddMember(context.Background(), f.PublicID, sku.PublicID, 1)
			errs <- err
		}(family)
	}
	wg.Wait()
	close(errs)
	successes := 0
	for err := range errs {
		if err == nil {
			successes++
		} else if !errors.Is(err, ErrSKUAlreadyInModelFamily) && !errors.Is(err, ErrMembershipConflict) {
			t.Fatalf("unexpected concurrent error: %v", err)
		}
	}
	if successes != 1 {
		t.Fatalf("successful adds = %d", successes)
	}
	var active int64
	if err := db.Model(&models.ModelFamilyMember{}).Where("sk_uid = ? AND removed_at IS NULL", sku.ID).Count(&active).Error; err != nil {
		t.Fatal(err)
	}
	if active != 1 {
		t.Fatalf("active memberships = %d", active)
	}
}

func ExampleModelFamilyService_Create() { fmt.Print("") }
