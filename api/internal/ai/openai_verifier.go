package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
)

const maxVerificationResponseBytes = 1 << 20

type HTTPProviderVerifier struct {
	baseURL string
	client  *http.Client
}

func NewHTTPProviderVerifier(baseURL string, client *http.Client) *HTTPProviderVerifier {
	if client == nil {
		client = http.DefaultClient
	}
	return &HTTPProviderVerifier{
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  client,
	}
}

func (v *HTTPProviderVerifier) Verify(ctx context.Context, apiKey string) (ProviderVerification, error) {
	response, err := v.modelsResponse(ctx, apiKey)
	if err != nil {
		return ProviderVerification{}, err
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxVerificationResponseBytes))
	return ProviderVerification{Authenticated: true}, nil
}

func (v *HTTPProviderVerifier) ListModels(ctx context.Context, apiKey string) ([]ProviderModel, error) {
	response, err := v.modelsResponse(ctx, apiKey)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxVerificationResponseBytes+1))
	if err != nil || len(raw) > maxVerificationResponseBytes {
		return nil, fmt.Errorf("list models: invalid provider response")
	}
	var payload struct {
		Data []struct {
			ID      string `json:"id"`
			OwnedBy string `json:"owned_by"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("list models: invalid provider response")
	}
	seen := make(map[string]struct{}, len(payload.Data))
	models := make([]ProviderModel, 0, len(payload.Data))
	for _, item := range payload.Data {
		id := strings.TrimSpace(item.ID)
		ownedBy := strings.TrimSpace(item.OwnedBy)
		if id == "" || len(id) > 200 || len(ownedBy) > 200 {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		models = append(models, ProviderModel{ID: id, OwnedBy: ownedBy})
	}
	sort.Slice(models, func(i, j int) bool { return models[i].ID < models[j].ID })
	return models, nil
}

func (v *HTTPProviderVerifier) modelsResponse(ctx context.Context, apiKey string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.baseURL+"/models", nil)
	if err != nil {
		return nil, fmt.Errorf("models request: create provider request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	response, err := v.client.Do(req)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, fmt.Errorf("models request: provider request failed")
	}

	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		_ = response.Body.Close()
		return nil, ErrCredentialVerification
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		_ = response.Body.Close()
		return nil, fmt.Errorf("models request: provider status %d", response.StatusCode)
	}
	return response, nil
}
