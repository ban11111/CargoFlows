package sop

import "cargoflow/api/internal/models"

type ViewInput struct {
	Role           models.SOPViewRole
	Kind           models.SOPViewKind
	NameZH         string
	NameEN         string
	InstructionZH  string
	InstructionEN  string
	Required       bool
	CameraPosition Vector3
	ImageUp        Vector3
	Target         Vector3
	Composition    models.Composition
}

var presetCatalog = map[string]ViewInput{
	"reference_front": {
		Role: models.SOPViewReferenceFront, Kind: models.SOPViewStandard,
		NameZH: "正面", NameEN: "Front",
		InstructionZH: "将商品正面对准相机，并确保商品顶部朝向画面顶部。",
		InstructionEN: "Face the product toward the camera and keep its top aligned with the top of the image.",
		Required:      true, CameraPosition: Vector3{0, 0, 1}, ImageUp: Vector3{1, 0, 0},
		Composition: defaultComposition(),
	},
	"back": {
		Role: models.SOPViewCapture, Kind: models.SOPViewStandard,
		NameZH: "背面", NameEN: "Back", Required: true,
		CameraPosition: Vector3{0, 0, -1}, ImageUp: Vector3{1, 0, 0},
		Composition: defaultComposition(),
	},
	"left": {
		Role: models.SOPViewCapture, Kind: models.SOPViewStandard,
		NameZH: "左侧", NameEN: "Left", Required: true,
		CameraPosition: Vector3{0, 1, 0}, ImageUp: Vector3{1, 0, 0},
		Composition: defaultComposition(),
	},
	"bottom": {
		Role: models.SOPViewCapture, Kind: models.SOPViewStandard,
		NameZH: "底部", NameEN: "Bottom", Required: true,
		CameraPosition: Vector3{-1, 0, 0}, ImageUp: Vector3{0, 1, 0},
		Composition: defaultComposition(),
	},
	"right": {
		Role: models.SOPViewCapture, Kind: models.SOPViewStandard,
		NameZH: "右侧", NameEN: "Right", Required: true,
		CameraPosition: Vector3{0, -1, 0}, ImageUp: Vector3{-1, 0, 0},
		Composition: defaultComposition(),
	},
	"top": {
		Role: models.SOPViewCapture, Kind: models.SOPViewStandard,
		NameZH: "顶部", NameEN: "Top", Required: true,
		CameraPosition: Vector3{1, 0, 0}, ImageUp: Vector3{0, -1, 0},
		Composition: defaultComposition(),
	},
	"detail_label": {
		Role: models.SOPViewCapture, Kind: models.SOPViewDetail,
		NameZH: "标签细节", NameEN: "Label Detail", Required: false,
		CameraPosition: Vector3{0, 0, 1}, ImageUp: Vector3{1, 0, 0},
		Composition: defaultComposition(),
	},
	"packaging_front": {
		Role: models.SOPViewCapture, Kind: models.SOPViewStandard,
		NameZH: "包装正面", NameEN: "Packaging Front",
		InstructionZH: "完整居中拍摄包装正面，确保品牌与标签清晰可读。",
		InstructionEN: "Center the complete package front and keep branding and labels legible.",
		Required:      false, CameraPosition: Vector3{0, 0, 1}, ImageUp: Vector3{1, 0, 0}, Target: Vector3{0, 0, 0},
		Composition: models.Composition{FrameOccupancy: 0.85, AspectRatio: "1:1", AllowRotationCorrection: true, AllowMirror: false},
	},
}

func PresetByKey(key string) (ViewInput, bool) {
	preset, ok := presetCatalog[key]
	return preset, ok
}

func defaultComposition() models.Composition {
	return models.Composition{
		FrameOccupancy:          0.85,
		AspectRatio:             "1:1",
		AllowRotationCorrection: true,
		AllowMirror:             false,
	}
}
