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
	"unicode/utf8"

	"cargoflows/api/internal/models"
)

func imagePromptFixture() (ProductSnapshotV1, SlotFacts) {
	snapshot, _ := textPromptFixture(models.AIContentSlotTitle)
	slot := SlotFacts{
		PublicID: "77777777-7777-4777-8777-777777777777", SlotKey: "hero", Kind: models.AIContentSlotImage,
		Name: LocalizedNameFacts{ZH: "Lazada 主图", EN: "Lazada hero"}, Description: LocalizedNameFacts{ZH: "展示核心卖点", EN: "Show the main selling point"},
		Sequence: 1, PromptFragment: "为 {{sku.code}} 制作忠实的 {{style.name}} 商品图，用于 {{target_platform}}。",
		Constraints:      json.RawMessage(`{"required_views":["reference_front"],"preserve_labels":true}`),
		GenerationConfig: json.RawMessage(`{"candidate_count":2,"size":"1024x1024","quality":"medium","style":"soft_studio","allow_user_extra_prompt":true,"allowed_candidate_count":[1,2,3,4],"allowed_sizes":["1024x1024","1536x1024"],"allowed_qualities":["medium","high"],"allowed_styles":["soft_studio","natural_daylight"]}`),
		LayoutConfig:     json.RawMessage(`{"text_safe_area":{"x":0.08,"y":0.08,"width":0.84,"height":0.28},"selling_point_focus":"slim transparent profile"}`),
	}
	snapshot.Template.PlatformPrompt = "为 {{product.brand}} 在 {{target_platform}} 制作商品详情图，清楚介绍商品并保持有辨识度的视觉风格。"
	snapshot.Template.SelectedSlots = []SlotFacts{slot}
	snapshot.SelectedAssets = []AssetFacts{
		{PublicID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", MIMEType: "image/png", Width: 1024, Height: 1024, ByteCount: 2048, SHA256: strings.Repeat("a", 64), CapturedAt: time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC), View: AssetViewFacts{PublicID: snapshot.SOP.Views[0].PublicID, PresetKey: "reference_front", Name: LocalizedNameFacts{ZH: "正面", EN: "Front"}, Role: models.SOPViewReferenceFront, ViewKind: models.SOPViewStandard, Instruction: LocalizedNameFacts{ZH: "正面拍摄", EN: "Front capture"}, CameraPositionDirection: VectorFacts{Z: 1}, ImageUpDirection: VectorFacts{X: 1}, Composition: models.Composition{FrameOccupancy: .85, AspectRatio: "1:1"}}},
		{PublicID: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", MIMEType: "image/png", Width: 1024, Height: 1024, ByteCount: 4096, SHA256: strings.Repeat("b", 64), View: AssetViewFacts{PublicID: "88888888-8888-4888-8888-888888888888", PresetKey: "detail", Name: LocalizedNameFacts{ZH: "细节", EN: "Detail"}, Role: models.SOPViewCapture, ViewKind: models.SOPViewDetail, Instruction: LocalizedNameFacts{ZH: "展示边缘", EN: "Show edge"}, CameraPositionDirection: VectorFacts{Y: 1}, ImageUpDirection: VectorFacts{X: 1}, Composition: models.Composition{FrameOccupancy: .75, AspectRatio: "1:1"}}},
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
	for _, required := range []string{"目标 SKU", "原有标签", "颜色", "比例", "包装款式", "绝不能从内侧搬到外侧", "只对其 view 元数据", "allow_mirror=false", "pcs_object_v1", "camera_position_direction", "视点与构图", "$input.slot.layout", "$input.request.user_instruction"} {
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

func TestCompileImagePromptEnforcesEnglishFirstBilingualVisibleText(t *testing.T) {
	snapshot, slot := imagePromptFixture()
	snapshot.Schema = ProductSnapshotSchemaV2
	snapshot.Locale = "en"
	snapshot.OutputLocales = []string{"en", "zh-CN"}
	compiled, err := CompileImagePrompt(snapshot, slot, ImageTurnInput{Operation: models.AIExecutionGenerate, ThreadPublicID: "thread-a"})
	if err != nil {
		t.Fatal(err)
	}
	if compiled.CompilerVersion != ImagePromptCompilerVersion || compiled.LayerVersions.Language != ImageLanguagePolicyVersion || !strings.Contains(compiled.Instructions, "English 为主并排在前") || !strings.Contains(compiled.Instructions, "简体中文紧随其后") || !strings.Contains(compiled.Instructions, "符合 L0-L1b") {
		t.Fatalf("language policy missing: %#v", compiled)
	}
}

func TestImagesAPIPromptDropsRedundantTaskBriefAtProviderLimit(t *testing.T) {
	prompt := CompiledImagePrompt{
		Instructions:         strings.Repeat("i", 10000),
		TaskBrief:            "unique-task-brief-" + strings.Repeat("t", 8000),
		NormalizedInputJSON:  json.RawMessage(`{"facts":"` + strings.Repeat("f", 12000) + `"}`),
		OrderedInputListJSON: json.RawMessage(`[{"notes":"` + strings.Repeat("o", 3000) + `"}]`),
	}

	full := prompt.Instructions + "\n\n" + prompt.ProviderInputText()
	got := prompt.ImagesAPIPrompt()
	if utf8.RuneCountInString(full) <= maxImagesAPIPromptCharacters {
		t.Fatalf("test fixture does not exceed limit: %d", utf8.RuneCountInString(full))
	}
	if utf8.RuneCountInString(got) > maxImagesAPIPromptCharacters || strings.Contains(got, "unique-task-brief") {
		t.Fatalf("compact prompt length=%d contains task brief=%v", utf8.RuneCountInString(got), strings.Contains(got, "unique-task-brief"))
	}
	for _, required := range []string{"<normalized_input_json>", "<ordered_input_list_json>", strings.Repeat("i", 100)} {
		if !strings.Contains(got, required) {
			t.Fatalf("compact prompt missing %q", required)
		}
	}
}

func TestCompileImagePromptRestrictsVisibleTextToSelectedSingleLanguage(t *testing.T) {
	for _, locale := range []string{"en", "zh-CN"} {
		t.Run(locale, func(t *testing.T) {
			snapshot, slot := imagePromptFixture()
			snapshot.Schema, snapshot.Locale, snapshot.OutputLocales = ProductSnapshotSchemaV2, locale, []string{locale}
			compiled, err := CompileImagePrompt(snapshot, slot, ImageTurnInput{Operation: models.AIExecutionGenerate, ThreadPublicID: "thread-a"})
			if err != nil {
				t.Fatal(err)
			}
			selected := map[string]string{"en": "只能使用 English", "zh-CN": "只能使用简体中文"}[locale]
			if !strings.Contains(compiled.Instructions, selected) {
				t.Fatalf("single-language image policy missing: %s", compiled.Instructions)
			}
		})
	}
}

func TestCompileImagePromptPlacesRecolorableBrandIconAfterProductSources(t *testing.T) {
	snapshot, slot := imagePromptFixture()
	snapshot.BrandIcons = []BrandIconFacts{{PublicID: "cccccccc-cccc-4ccc-8ccc-cccccccccccc", Name: "Primary wordmark", Notes: "Horizontal", MIMEType: "image/png", Width: 400, Height: 120, ByteCount: 2048, SHA256: strings.Repeat("c", 64)}}
	compiled, err := CompileImagePrompt(snapshot, slot, ImageTurnInput{Operation: models.AIExecutionGenerate, ThreadPublicID: "thread-brand"})
	if err != nil {
		t.Fatal(err)
	}
	ordered := string(compiled.OrderedInputListJSON)
	lastSource := strings.LastIndex(ordered, `"kind":"product_visual"`)
	brand := strings.Index(ordered, `"kind":"brand_icon_reference"`)
	if lastSource < 0 || brand <= lastSource {
		t.Fatalf("brand icon order is invalid: %s", ordered)
	}
	for _, required := range []string{"可为对比度调色", "不得重绘", "不得定义商品", "宽高比"} {
		if !strings.Contains(compiled.Instructions, required) {
			t.Fatalf("brand instructions missing %q", required)
		}
	}
}

func TestCompileImagePromptExcludesPhoneCaseInteriorFromExteriorBinaryInputs(t *testing.T) {
	snapshot, slot := imagePromptFixture()
	snapshot.Schema = ProductSnapshotSchemaV2
	snapshot.Locale = "zh-CN"
	snapshot.OutputLocales = []string{"zh-CN"}
	snapshot.SKU.CompatibleDeviceModel = "iPhone 17 Pro"
	snapshot.SelectedAssets[1].View.PresetKey = "back"
	snapshot.SelectedAssets[1].View.Name = LocalizedNameFacts{ZH: "背面", EN: "Back"}

	compiled, err := CompileImagePrompt(snapshot, slot, ImageTurnInput{Operation: models.AIExecutionGenerate, ThreadPublicID: "thread-surface"})
	if err != nil {
		t.Fatal(err)
	}
	ordered := string(compiled.OrderedInputListJSON)
	if count, err := compiled.ExpectedInputCount(); err != nil || count != 1 {
		t.Fatalf("input count = %d, error = %v", count, err)
	}
	for _, required := range []string{`"preset_key":"back"`, `"surface_role":"customer_facing_exterior"`, "表面角色=customer_facing_exterior"} {
		if !strings.Contains(compiled.ImagesAPIPrompt(), required) {
			t.Fatalf("surface-filtered prompt missing %q: %s", required, compiled.ImagesAPIPrompt())
		}
	}
	if strings.Contains(ordered, `"preset_key":"reference_front"`) || strings.Contains(ordered, `"surface_role":"device_facing_interior"`) {
		t.Fatalf("device-facing interior leaked into exterior binary inputs: %s", ordered)
	}

	slot.Constraints = json.RawMessage(`{"include_device_facing_interior_reference":true}`)
	compiled, err = CompileImagePrompt(snapshot, slot, ImageTurnInput{Operation: models.AIExecutionGenerate, ThreadPublicID: "thread-surface-opt-in"})
	if err != nil {
		t.Fatal(err)
	}
	if count, err := compiled.ExpectedInputCount(); err != nil || count != 2 || !strings.Contains(string(compiled.OrderedInputListJSON), `"surface_role":"device_facing_interior"`) {
		t.Fatalf("explicit interior opt-in was not preserved: count=%d err=%v ordered=%s", count, err, compiled.OrderedInputListJSON)
	}
}

func TestCompileImagePromptSeparatesBrandStructureStyleAndCustomStylePermissions(t *testing.T) {
	snapshot, slot := imagePromptFixture()
	slot.GenerationConfig = json.RawMessage(`{"candidate_count":1,"size":"1024x1024","quality":"medium","style":"art_directed","allowed_styles":["art_directed"]}`)
	snapshot.BrandIcons = []BrandIconFacts{{PublicID: "brand-a", Name: "Wordmark"}}
	snapshot.StructureReferences = []StructureReferenceFacts{{PublicID: "structure-a", Role: "same_model_side_geometry", ForbiddenAttributes: json.RawMessage(`{"color":true,"labels":true}`)}}
	snapshot.StyleReferences = []StyleReferenceFacts{{PublicID: "style-a", Description: LocalizedNameFacts{ZH: "冷色留白", EN: "Cool negative space"}}}
	compiled, err := CompileImagePrompt(snapshot, slot, ImageTurnInput{Operation: models.AIExecutionGenerate, ThreadPublicID: "thread-role-map"})
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		"只有图片 1, 图片 2可定义主体身份",
		"图片 3: 仅限品牌标记",
		"图片 4: 仅限声明角色 same_model_side_geometry 的同机型结构",
		"图片 5: 仅限已净化的风格",
		"风格预设——只控制视觉处理\n\"art_directed\"",
		"不得重定义主体",
	} {
		if !strings.Contains(compiled.TaskBrief, required) {
			t.Fatalf("task brief missing %q: %s", required, compiled.TaskBrief)
		}
	}
	if count, err := compiled.ExpectedInputCount(); err != nil || count != 5 {
		t.Fatalf("input count = %d, error = %v", count, err)
	}
}

func TestCompileImagePromptCombinesChosenRequirementsOnOneCanvas(t *testing.T) {
	snapshot, hero := imagePromptFixture()
	detail := hero
	detail.PublicID = "99999999-9999-4999-8999-999999999999"
	detail.SlotKey = "detail"
	detail.Name = LocalizedNameFacts{ZH: "细节卖点", EN: "Detail benefits"}
	detail.PromptFragment = "展示 {{sku.code}} 的边缘保护和按键触感细节。"
	detail.LayoutConfig = json.RawMessage(`{"detail_inset":"right"}`)
	snapshot.Template.SelectedSlots = []SlotFacts{hero, detail}
	count := 3
	hero.CanvasKey = "canvas-a"
	hero.CanvasGeneration = &GenerationOverride{CandidateCount: &count}
	heroRequirement := hero
	heroRequirement.CanvasKey = ""
	heroRequirement.CanvasGeneration = nil
	hero.CompositeRequirements = []SlotFacts{heroRequirement, detail}

	compiled, err := CompileImagePrompt(snapshot, hero, ImageTurnInput{Operation: models.AIExecutionGenerate, ThreadPublicID: "thread-composite"})
	if err != nil {
		t.Fatal(err)
	}
	if compiled.CandidateCount != 3 {
		t.Fatalf("canvas-specific generation override was ignored: %#v", compiled)
	}
	joined := compiled.Instructions + string(compiled.NormalizedInputJSON)
	for _, required := range []string{"一张同时满足", "全部条目", "[复合要求 hero /", "[复合要求 detail /", "边缘保护", `"canvas_key":"canvas-a"`, `"composite_requirements"`, `"slot_key":"hero"`, `"slot_key":"detail"`} {
		if !strings.Contains(joined, required) {
			t.Fatalf("composite prompt missing %q: %s", required, joined)
		}
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

func TestCompileImagePromptExpandsKnownStyleAndSeparatesInformationSources(t *testing.T) {
	snapshot, slot := imagePromptFixture()
	slot.GenerationConfig = json.RawMessage(`{"candidate_count":1,"size":"1024x1024","quality":"high","style":"premium_dark","allowed_styles":["premium_dark"]}`)
	snapshot.SelectedAssets[1].SourceType = AssetSourceProductInformation
	snapshot.SelectedAssets[1].View.PresetKey = "supplemental_info"

	compiled, err := CompileImagePrompt(snapshot, slot, ImageTurnInput{Operation: models.AIExecutionGenerate, ThreadPublicID: "thread-a"})
	if err != nil {
		t.Fatal(err)
	}
	input := string(compiled.NormalizedInputJSON)
	ordered := string(compiled.OrderedInputListJSON)
	if !strings.Contains(input, `"style":"premium_dark"`) || !strings.Contains(input, `"style_instructions":"媒介：高端写实影棚摄影`) {
		t.Fatalf("known style was not expanded: %s", input)
	}
	if !strings.Contains(ordered, `"kind":"product_visual"`) || !strings.Contains(ordered, `"kind":"product_information"`) {
		t.Fatalf("source roles were not separated: %s", ordered)
	}
}

func TestImageStyleCatalogContainsStablePresetSet(t *testing.T) {
	want := []string{"clean_white_background", "soft_studio", "high_key_airy", "warm_neutral", "premium_dark", "luxury_editorial", "minimal_gradient", "bold_color_block", "vibrant_pop", "pastel_soft", "natural_daylight", "cozy_home", "modern_urban", "outdoor_active", "flat_lay", "macro_material", "technical_3d", "isometric_illustration", "clean_infographic", "seasonal_campaign"}
	if len(ImageStyleCatalog) != len(want) {
		t.Fatalf("style count = %d, want %d", len(ImageStyleCatalog), len(want))
	}
	for _, key := range want {
		instruction := ImageStyleCatalog[key]
		for _, section := range []string{"媒介：", "背景：", "光线：", "构图：", "道具："} {
			if !strings.Contains(instruction, section) {
				t.Errorf("style %q is missing %q: %s", key, section, instruction)
			}
		}
		if !strings.Contains(instruction, "禁止") && !strings.Contains(instruction, "不得") {
			t.Errorf("style %q is missing an exclusion rule: %s", key, instruction)
		}
	}
}

func TestCompileImagePromptRejectsUnsafeOrInvalidInput(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*ProductSnapshotV1, *SlotFacts, *ImageTurnInput)
		want   error
	}{
		{"invalid v2 output locales", func(snapshot *ProductSnapshotV1, _ *SlotFacts, _ *ImageTurnInput) {
			snapshot.Schema, snapshot.Locale, snapshot.OutputLocales = ProductSnapshotSchemaV2, "zh-CN", []string{"zh-CN", "en"}
		}, ErrImagePromptSnapshotInvalid},
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

func TestCompileImagePromptLabelsExternalReferencesAsUntrustedInspiration(t *testing.T) {
	snapshot, slot := imagePromptFixture()
	snapshot.ReferenceSOPs = []ReferenceSOPFacts{{PublicID: "sop-a", VersionPublicID: "sop-version-a", VersionNumber: 1, Name: LocalizedNameFacts{ZH: "手机壳装机参考", EN: "Phone case fitted reference"}, Description: LocalizedNameFacts{ZH: "只参考装机关系", EN: "Fitted relationship only"}}}
	snapshot.ExternalReferences = []ExternalReferenceFacts{
		{PublicID: "external-a", VersionPublicID: "sop-version-a", Purpose: models.AIReferenceUsageEffect, Caption: LocalizedNameFacts{ZH: "另一款手机壳套机", EN: "Another phone case fitted"}, AllowedGuidance: LocalizedNameFacts{ZH: "仅参考装机比例和姿态", EN: "Installed proportion and pose only"}, ForbiddenGuidance: LocalizedNameFacts{ZH: "禁止继承外形、颜色、开孔、品牌、设备、配件和包装", EN: "Do not inherit shape, color, cutouts, brand, device, accessories, or packaging"}, SourceName: "Competitor", SHA256: strings.Repeat("a", 64)},
		{PublicID: "external-copy", Purpose: models.AIReferenceCopyInspiration, Caption: LocalizedNameFacts{ZH: "文案", EN: "Copy"}, AllowedGuidance: LocalizedNameFacts{ZH: "语气", EN: "Tone"}, ForbiddenGuidance: LocalizedNameFacts{ZH: "原文", EN: "Wording"}, SHA256: strings.Repeat("b", 64)},
	}
	compiled, err := CompileImagePrompt(snapshot, slot, ImageTurnInput{Operation: models.AIExecutionGenerate, ThreadPublicID: "thread-a"})
	if err != nil {
		t.Fatal(err)
	}
	joined := compiled.ImagesAPIPrompt()
	for _, required := range []string{"图片 1: 目标 SKU 商品图", "原始参考图未作为二进制输入", "仅参考装机比例和姿态", "禁止继承外形、颜色、开孔、品牌、设备、配件和包装", "只参考装机关系"} {
		if !strings.Contains(joined, required) {
			t.Fatalf("external safety metadata missing %q: %s", required, joined)
		}
	}
	for _, forbidden := range []string{"Installed proportion and pose only", "Do not inherit shape, color, cutouts", "Fitted relationship only", "Another phone case fitted"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("English reference SOP prompt was not compacted %q: %s", forbidden, joined)
		}
	}
	if strings.Contains(joined, "external-copy") || strings.Contains(joined, "external_reference_copy_inspiration") {
		t.Fatalf("copy inspiration leaked into image prompt: %s", joined)
	}
	if got := len(imageGenerationExternalReferences(snapshot.ExternalReferences)); got != 1 {
		t.Fatalf("image references = %d, want 1", got)
	}
	if got := len(imageGenerationBinaryExternalReferences(snapshot.ExternalReferences)); got != 0 {
		t.Fatalf("binary image references = %d, want 0", got)
	}
	if count, err := compiled.ExpectedInputCount(); err != nil || count != 2 {
		t.Fatalf("input count = %d, error = %v", count, err)
	}
	if ordered := string(compiled.OrderedInputListJSON); strings.Contains(ordered, "external_reference_") || strings.Contains(ordered, `"source_ref":"external_1"`) {
		t.Fatalf("raw external reference leaked into binary input order: %s", ordered)
	}
}
