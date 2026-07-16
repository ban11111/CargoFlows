package ai

import (
	"context"
	"fmt"
	"io"
	"net/http"
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
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.baseURL+"/models", nil)
	if err != nil {
		return ProviderVerification{}, fmt.Errorf("verify credential: create provider request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	response, err := v.client.Do(req)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ProviderVerification{}, ctxErr
		}
		return ProviderVerification{}, fmt.Errorf("verify credential: provider request failed")
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxVerificationResponseBytes))

	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return ProviderVerification{}, ErrCredentialVerification
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return ProviderVerification{}, fmt.Errorf("verify credential: provider status %d", response.StatusCode)
	}
	return ProviderVerification{Authenticated: true}, nil
}
