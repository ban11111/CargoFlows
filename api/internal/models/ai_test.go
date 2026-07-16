package models

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
)

func TestAIModelsDoNotSerializeInternalRelationshipIDs(t *testing.T) {
	values := []uint{910001, 910002, 910003, 910004, 910005, 910006, 910007, 910008, 910009, 910010, 910011, 910012, 910013, 910014, 910015, 910016, 910017}
	models := []any{
		AIContentTemplate{CreatedByID: values[0]},
		AIContentTemplateVersion{AIContentTemplateID: values[1], CreatedByID: values[2], PublishedByID: &values[3]},
		AIContentSlot{AIContentTemplateVersionID: values[4]},
		AIJob{SKUID: values[5], AIContentTemplateVersionID: values[6], CreatedByID: values[7]},
		AIJobItem{AIJobID: values[8], AIContentSlotID: values[9], CurrentCandidateID: &values[10], EffectiveApprovedResultID: &values[11]},
		AIExecution{AIJobItemID: values[12], ParentExecutionID: &values[13]},
		AIAuditEvent{ActorID: &values[14], AIJobID: &values[8], AIJobItemID: &values[9], AIExecutionID: &values[15]},
		AIUsageLedger{AIExecutionID: values[16]},
	}

	encoded, err := json.Marshal(models)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for _, key := range []string{
		"created_by_id", "published_by_id", "sku_id", "template_version_id",
		"content_slot_id", "current_candidate_id", "effective_approved_result_id",
		"job_id", "job_item_id", "parent_execution_id", "actor_id", "execution_id",
	} {
		if strings.Contains(text, `"`+key+`"`) {
			t.Fatalf("serialized internal relationship key %q: %s", key, text)
		}
	}
	for _, value := range values {
		if strings.Contains(text, strconv.FormatUint(uint64(value), 10)) {
			t.Fatalf("serialized internal relationship value %d: %s", value, text)
		}
	}
}
