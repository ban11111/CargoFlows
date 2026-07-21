package app

import "encoding/json"

func safeAssetSourceSummary(raw []byte) map[string]string {
	result := map[string]string{}
	var values map[string]any
	if json.Unmarshal(raw, &values) != nil {
		return result
	}
	for _, key := range []string{"job_id", "job_item_id", "image_result_id", "model", "api_mode", "submitted_by"} {
		if value, ok := values[key].(string); ok && value != "" {
			result[key] = value
		}
	}
	return result
}
