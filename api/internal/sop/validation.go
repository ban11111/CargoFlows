package sop

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"

	"cargoflow/api/internal/models"
)

type LocalizedMessage struct {
	ZHCN string `json:"zh-CN"`
	EN   string `json:"en"`
}

type ValidationError struct {
	Code    string           `json:"code"`
	Path    string           `json:"path"`
	Message LocalizedMessage `json:"message"`
}

var aspectRatioPattern = regexp.MustCompile(`^[1-9][0-9]*:[1-9][0-9]*$`)

func ValidateVersion(version models.SOPVersion) []ValidationError {
	validationErrors := make([]ValidationError, 0)
	if strings.TrimSpace(version.NameZH) == "" {
		validationErrors = append(validationErrors, validationError("name_zh_required", "name.zh-CN", "中文名称不能为空。", "Chinese name is required."))
	}
	if strings.TrimSpace(version.NameEN) == "" {
		validationErrors = append(validationErrors, validationError("name_en_required", "name.en", "英文名称不能为空。", "English name is required."))
	}

	validationErrors = append(validationErrors, validateReferenceFront(version.Views)...)
	validationErrors = append(validationErrors, validateSequences(version.Views)...)
	for index, view := range version.Views {
		validationErrors = append(validationErrors, validateView(index, view)...)
	}
	return validationErrors
}

func validateReferenceFront(views []models.SOPView) []ValidationError {
	referenceCount := 0
	validationErrors := make([]ValidationError, 0)
	for index, view := range views {
		if view.Role != models.SOPViewReferenceFront {
			continue
		}
		referenceCount++
		camera, imageUp, target := viewVectors(view)
		if view.Sequence != 1 || view.ViewKind != models.SOPViewStandard || !view.Required ||
			camera != (Vector3{0, 0, 1}) || imageUp != (Vector3{1, 0, 0}) || target != (Vector3{}) {
			validationErrors = append(validationErrors, validationError(
				"reference_front_invalid", fmt.Sprintf("views[%d]", index),
				"参考正面必须保持固定的顺序、类型、必拍状态和姿态。",
				"Reference front must retain its fixed sequence, kind, required state, and pose.",
			))
		}
	}
	if referenceCount != 1 {
		validationErrors = append(validationErrors, validationError(
			"reference_front_count", "views", "必须且只能有一个参考正面。", "Exactly one reference front is required.",
		))
	}
	return validationErrors
}

func validateSequences(views []models.SOPView) []ValidationError {
	counts := make(map[int]int, len(views))
	for _, view := range views {
		counts[view.Sequence]++
	}
	validationErrors := make([]ValidationError, 0)
	for index, view := range views {
		if view.Sequence < 1 || view.Sequence > len(views) || counts[view.Sequence] != 1 {
			validationErrors = append(validationErrors, validationError(
				"sequence_invalid", fmt.Sprintf("views[%d].sequence", index),
				"视图顺序必须从 1 开始、连续且唯一。", "View sequences must start at 1 and be contiguous and unique.",
			))
		}
	}
	return validationErrors
}

func validateView(index int, view models.SOPView) []ValidationError {
	basePath := fmt.Sprintf("views[%d]", index)
	validationErrors := make([]ValidationError, 0)
	if strings.TrimSpace(view.NameZH) == "" {
		validationErrors = append(validationErrors, validationError("view_name_zh_required", basePath+".name.zh-CN", "视图中文名称不能为空。", "View Chinese name is required."))
	}
	if strings.TrimSpace(view.NameEN) == "" {
		validationErrors = append(validationErrors, validationError("view_name_en_required", basePath+".name.en", "视图英文名称不能为空。", "View English name is required."))
	}

	camera, imageUp, target := viewVectors(view)
	validationErrors = append(validationErrors, validatePose(basePath, camera, imageUp)...)
	if view.ViewKind == models.SOPViewStandard && target != (Vector3{}) {
		validationErrors = append(validationErrors, validationError("standard_target_not_origin", basePath+".pose.target", "标准视图必须以原点为目标。", "Standard views must target the origin."))
	}
	if view.ViewKind == models.SOPViewDetail && !targetWithinDetailBounds(target) {
		validationErrors = append(validationErrors, validationError("detail_target_out_of_bounds", basePath+".pose.target", "细节视图目标必须位于归一化边界内。", "Detail view target must be within the normalized bounds."))
	}

	composition := view.Composition
	if !isFinite(composition.FrameOccupancy) || composition.FrameOccupancy <= 0 || composition.FrameOccupancy > 1 {
		validationErrors = append(validationErrors, validationError("frame_occupancy_invalid", basePath+".composition.frame_occupancy", "画面占比必须大于 0 且不超过 1。", "Frame occupancy must be greater than 0 and at most 1."))
	}
	if !aspectRatioPattern.MatchString(composition.AspectRatio) {
		validationErrors = append(validationErrors, validationError("aspect_ratio_invalid", basePath+".composition.aspect_ratio", "宽高比必须为正整数的宽:高格式。", "Aspect ratio must use positive integer width:height format."))
	}
	if composition.AllowMirror {
		validationErrors = append(validationErrors, validationError("allow_mirror_invalid", basePath+".composition.allow_mirror", "不允许镜像商品图片。", "Mirrored product images are not allowed."))
	}
	return validationErrors
}

func validatePose(basePath string, camera, imageUp Vector3) []ValidationError {
	validationErrors := make([]ValidationError, 0)
	cameraPath := basePath + ".pose.camera_position_direction"
	imageUpPath := basePath + ".pose.image_up_direction"
	if err := vectorValidity(camera); err != nil {
		validationErrors = append(validationErrors, poseVectorError(err, cameraPath))
	}
	if err := vectorValidity(imageUp); err != nil {
		validationErrors = append(validationErrors, poseVectorError(err, imageUpPath))
	}
	if len(validationErrors) != 0 {
		return validationErrors
	}
	canonical, err := CanonicalizePose(camera, imageUp)
	if errors.Is(err, ErrParallelVectors) {
		validationErrors = append(validationErrors, validationError(
			"pose_vectors_parallel", imageUpPath, "相机方向与图片向上方向不能平行。", "Camera direction and image-up direction cannot be parallel.",
		))
		return validationErrors
	}
	if err != nil || vectorValidity(canonical.CameraPosition) != nil || vectorValidity(canonical.ImageUp) != nil {
		validationErrors = append(validationErrors, validationError(
			"pose_vector_invalid", cameraPath, "方向向量无法转换为有效的规范姿态。", "Direction vectors cannot be converted to a valid canonical pose.",
		))
	}
	return validationErrors
}

func vectorValidity(vector Vector3) error {
	for _, component := range vector {
		if !isFinite(component) {
			return ErrNonFiniteVector
		}
	}
	if math.Hypot(math.Hypot(vector[0], vector[1]), vector[2]) < 1e-9 {
		return ErrZeroVector
	}
	return nil
}

func poseVectorError(err error, path string) ValidationError {
	if errors.Is(err, ErrNonFiniteVector) {
		return validationError("pose_vector_non_finite", path, "方向向量必须只包含有限数值。", "Direction vector must contain only finite numbers.")
	}
	return validationError("pose_vector_zero", path, "方向向量不能为零向量。", "Direction vector cannot be zero.")
}

func viewVectors(view models.SOPView) (Vector3, Vector3, Vector3) {
	return Vector3{view.CameraPositionX, view.CameraPositionY, view.CameraPositionZ},
		Vector3{view.ImageUpX, view.ImageUpY, view.ImageUpZ},
		Vector3{view.TargetX, view.TargetY, view.TargetZ}
}

func targetWithinDetailBounds(target Vector3) bool {
	for _, component := range target {
		if !isFinite(component) || component < -0.5 || component > 0.5 {
			return false
		}
	}
	return true
}

func isFinite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func validationError(code, path, zhCN, en string) ValidationError {
	return ValidationError{Code: code, Path: path, Message: LocalizedMessage{ZHCN: zhCN, EN: en}}
}
