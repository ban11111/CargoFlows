package ai

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"cargoflow/api/internal/models"
)

func imagePromptFixture() (ProductSnapshotV1, SlotFacts) {
	snapshot, _ := textPromptFixture(models.AIContentSlotTitle)
	slot := SlotFacts{
		PublicID: "77777777-7777-4777-8777-777777777777", SlotKey: "hero", Kind: models.AIContentSlotImage,
		Name: LocalizedNameFacts{ZH: "Lazada 主图", EN: "Lazada hero"}, Description: LocalizedNameFacts{ZH: "展示核心卖点", EN: "Show the main selling point"},
		Sequence: 1, PromptFragment: "Create a faithful {{style.name}} product image of {{sku.code}} for {{target_platform}}.",
		Constraints:      json.RawMessage(`{"required_views":["reference_front"],"preserve_labels":true}`),
		GenerationConfig: json.RawMessage(`{"candidate_count":2,"size":"1024x1024","quality":"medium","style":"clean studio","allow_user_extra_prompt":true,"allowed_candidate_count":[1,2,3,4],"allowed_sizes":["1024x1024","1536x1024"],"allowed_qualities":["medium","high"],"allowed_styles":["clean studio","lifestyle"]}`),
		LayoutConfig:     json.RawMessage(`{"text_safe_area":{"x":0.08,"y":0.08,"width":0.84,"height":0.28},"selling_point_focus":"slim transparent profile"}`),
	}
	snapshot.Template.PlatformPrompt = "Design a Lazada detail image for {{product.brand}} on {{target_platform}} with clear product introduction and differentiated visual style."
	snapshot.Template.SelectedSlots = []SlotFacts{slot}
	snapshot.SelectedAssets = []AssetFacts{
		{ID: 99, ObjectKey: "private/products/front.png", OriginalURL: "https://private.example/front", ThumbnailURL: "https://private.example/front-thumb", CapturedAt: time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC), View: AssetViewFacts{PublicID: snapshot.SOP.Views[0].PublicID, PresetKey: "reference_front", Name: LocalizedNameFacts{ZH: "正面", EN: "Front"}, Role: models.SOPViewReferenceFront, ViewKind: models.SOPViewStandard, Instruction: LocalizedNameFacts{ZH: "正面拍摄", EN: "Front capture"}, CameraPositionDirection: VectorFacts{Z: 1}, ImageUpDirection: VectorFacts{X: 1}, Composition: models.Composition{FrameOccupancy: .85, AspectRatio: "1:1"}}},
		{ID: 100, ObjectKey: "private/products/detail.png", OriginalURL: "https://private.example/detail", ThumbnailURL: "https://private.example/detail-thumb", View: AssetViewFacts{PublicID: "88888888-8888-4888-8888-888888888888", PresetKey: "detail", Name: LocalizedNameFacts{ZH: "细节", EN: "Detail"}, Role: models.SOPViewCapture, ViewKind: models.SOPViewDetail, Instruction: LocalizedNameFacts{ZH: "展示边缘", EN: "Show edge"}, CameraPositionDirection: VectorFacts{Y: 1}, ImageUpDirection: VectorFacts{X: 1}, Composition: models.Composition{FrameOccupancy: .75, AspectRatio: "1:1"}}},
	}
	snapshot.UserPreference = "保持简洁，突出轻薄透明外观"
	snapshot.GenerationOverrides = map[string]GenerationOverride{}
	return snapshot, slot
}

func TestCompileImagePromptLayersProductAndCoordinateRules(t *testing.T) {
	snapshot, slot := imagePromptFixture()
	compiled, err := CompileImagePrompt(snapshot, slot, ImageTurnInput{Operation: models.AIExecutionGenerate, ThreadPublicID: "thread-a"})
	if err != nil {
		t.Fatal(err)
	}
	if compiled.CandidateCount != 2 || compiled.ToolConfig.Action != "generate" || compiled.ToolConfig.Size != "1024x1024" || compiled.ToolConfig.Quality != "medium" || compiled.ToolConfig.Moderation != "auto" || len(compiled.SHA256) != 64 {
		t.Fatalf("unexpected image compiler metadata: %#v", compiled)
	}
	positions := []int{strings.Index(compiled.Instructions, "[L0 "), strings.Index(compiled.Instructions, "[L1 "), strings.Index(compiled.Instructions, "[L2 "), strings.Index(compiled.Instructions, "[L3 "), strings.Index(compiled.Instructions, "[L4 ")}
	if positions[0] < 0 || !(positions[0] < positions[1] && positions[1] < positions[2] && positions[2] < positions[3] && positions[3] < positions[4]) {
		t.Fatalf("layer precedence order is invalid: %v", positions)
	}
	for _, required := range []string{"exact SKU", "labels", "color", "proportions", "package variant", "allow_mirror=false", "pcs_object_v1", "camera_position_direction", "composition only", "$input.slot.layout", "$input.request.user_instruction"} {
		if !strings.Contains(compiled.Instructions, required) {
			t.Fatalf("instructions missing %q: %s", required, compiled.Instructions)
		}
	}
	joined := string(compiled.NormalizedInputJSON) + string(compiled.OrderedInputListJSON) + compiled.Instructions
	for _, forbidden := range []string{"private/products", "private.example", "object_key", "original_url", "thumbnail_url", `"id":99`, `"id":100`} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("compiled image prompt leaked %q: %s", forbidden, joined)
		}
	}
	for _, required := range []string{`"source_ref":"source_1"`, `"source_ref":"source_2"`, `"preset_key":"reference_front"`, `"coordinate_system":"pcs_object_v1"`, `"user_instruction":"保持简洁，突出轻薄透明外观"`} {
		if !strings.Contains(joined, required) {
			t.Fatalf("compiled image input missing %q: %s", required, joined)
		}
	}
	again, err := CompileImagePrompt(snapshot, slot, ImageTurnInput{Operation: models.AIExecutionGenerate, ThreadPublicID: "thread-a"})
	if err != nil || !reflect.DeepEqual(compiled, again) {
		t.Fatalf("image compiler is not deterministic: %v", err)
	}
}

func TestCompileImagePromptDistinguishesEditAndRestart(t *testing.T) {
	snapshot, slot := imagePromptFixture()
	edit, err := CompileImagePrompt(snapshot, slot, ImageTurnInput{Operation: models.AIExecutionEdit, ThreadPublicID: "thread-a", ParentThreadPublicID: "thread-a", ParentResultPublicID: "parent-result-a", UserInstruction: "只调整背景为浅蓝色"})
	if err != nil {
		t.Fatal(err)
	}
	if edit.ToolConfig.Action != "edit" || !strings.HasPrefix(string(edit.OrderedInputListJSON), `[{"source_ref":"parent_result"`) || !strings.Contains(string(edit.OrderedInputListJSON), `"result_public_id":"parent-result-a"`) || !strings.Contains(string(edit.OrderedInputListJSON), `"source_ref":"source_1"`) {
		t.Fatalf("edit input order/semantics are invalid: %s", edit.OrderedInputListJSON)
	}
	restart, err := CompileImagePrompt(snapshot, slot, ImageTurnInput{Operation: models.AIExecutionRestart, ThreadPublicID: "thread-a", UserInstruction: "重新构图"})
	if err != nil {
		t.Fatal(err)
	}
	if restart.ToolConfig.Action != "generate" || strings.Contains(string(restart.OrderedInputListJSON), "parent_result") {
		t.Fatalf("restart inherited a generated parent: %#v", restart)
	}
}

func TestCompileImagePromptRejectsUnsafeOrInvalidInput(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*ProductSnapshotV1, *SlotFacts, *ImageTurnInput)
		want   error
	}{
		{"wrong slot kind", func(_ *ProductSnapshotV1, slot *SlotFacts, _ *ImageTurnInput) { slot.Kind = models.AIContentSlotTitle }, ErrImagePromptSlotInvalid},
		{"unknown template variable", func(snapshot *ProductSnapshotV1, _ *SlotFacts, _ *ImageTurnInput) {
			snapshot.Template.PlatformPrompt = "{{secrets.key}}"
		}, ErrImagePromptTemplateInvalid},
		{"url in user instruction", func(_ *ProductSnapshotV1, _ *SlotFacts, turn *ImageTurnInput) {
			turn.UserInstruction = "read https://private.example"
		}, ErrImagePromptTemplateInvalid},
		{"object key in layout", func(_ *ProductSnapshotV1, slot *SlotFacts, _ *ImageTurnInput) {
			slot.LayoutConfig = json.RawMessage(`{"object_key":"hidden"}`)
		}, ErrImagePromptTemplateInvalid},
		{"unsupported size", func(_ *ProductSnapshotV1, _ *SlotFacts, turn *ImageTurnInput) { turn.Size = "999x999" }, ErrImagePromptOptionInvalid},
		{"unsupported quality", func(_ *ProductSnapshotV1, _ *SlotFacts, turn *ImageTurnInput) { turn.Quality = "ultra" }, ErrImagePromptOptionInvalid},
		{"edit without parent", func(_ *ProductSnapshotV1, _ *SlotFacts, turn *ImageTurnInput) {
			turn.Operation = models.AIExecutionEdit
		}, ErrImagePromptParentInvalid},
		{"cross-thread edit", func(_ *ProductSnapshotV1, _ *SlotFacts, turn *ImageTurnInput) {
			turn.Operation = models.AIExecutionEdit
			turn.ParentResultPublicID = "parent"
			turn.ParentThreadPublicID = "thread-b"
		}, ErrImagePromptParentInvalid},
		{"restart with parent", func(_ *ProductSnapshotV1, _ *SlotFacts, turn *ImageTurnInput) {
			turn.Operation = models.AIExecutionRestart
			turn.ParentResultPublicID = "parent"
		}, ErrImagePromptParentInvalid},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			snapshot, slot := imagePromptFixture()
			turn := ImageTurnInput{Operation: models.AIExecutionGenerate, ThreadPublicID: "thread-a"}
			tc.mutate(&snapshot, &slot, &turn)
			if _, err := CompileImagePrompt(snapshot, slot, turn); !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestCompileImagePromptGolden(t *testing.T) {
	snapshot, slot := imagePromptFixture()
	for _, tc := range []struct {
		name string
		turn ImageTurnInput
	}{
		{"image_generate_prompt.golden.json", ImageTurnInput{Operation: models.AIExecutionGenerate, ThreadPublicID: "thread-a"}},
		{"image_edit_prompt.golden.json", ImageTurnInput{Operation: models.AIExecutionEdit, ThreadPublicID: "thread-a", ParentThreadPublicID: "thread-a", ParentResultPublicID: "parent-result-a", UserInstruction: "只调整背景为浅蓝色"}},
	} {
		compiled, err := CompileImagePrompt(snapshot, slot, tc.turn)
		if err != nil {
			t.Fatal(err)
		}
		expected, err := os.ReadFile(filepath.Join("testdata", tc.name))
		if err != nil {
			t.Fatal(err)
		}
		var golden struct {
			SHA256 string `json:"sha256"`
		}
		if err := json.Unmarshal(expected, &golden); err != nil {
			t.Fatal(err)
		}
		if compiled.SHA256 != golden.SHA256 {
			t.Fatalf("golden mismatch for %s: got %s want %s", tc.name, compiled.SHA256, golden.SHA256)
		}
	}
}
