package ai

import (
	"errors"
	"net/http"
	"testing"

	"cargoflows/api/internal/models"
)

func TestImageProviderFailureStateExplainsAmbiguousCause(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
		code string
	}{
		{"timeout", &ImageProviderError{Kind: ErrImageProviderAmbiguousTimeout}, "OpenAI image request timed out after it was sent; no response was received", "openai_timeout_ambiguous"},
		{"transport", &ImageProviderError{Kind: ErrImageProviderAmbiguousTransport}, "Connection to OpenAI was interrupted after the image request was sent; no response was received", "openai_transport_ambiguous"},
		{"server", &ImageProviderError{Kind: ErrImageProviderAmbiguousTransport, StatusCode: http.StatusBadGateway}, "OpenAI image API returned HTTP 502 before a result was confirmed", "openai_server_error_ambiguous"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			status, message := imageProviderFailureState(tc.err)
			if status != models.AIExecutionNeedsAttention || message != tc.want || failureCodeForSafeError(message) != tc.code {
				t.Fatalf("status=%q message=%q code=%q", status, message, failureCodeForSafeError(message))
			}
		})
	}
}

func TestTextProviderFailureStateExplainsAmbiguousCause(t *testing.T) {
	tests := []struct {
		err  error
		want string
	}{
		{&TextProviderError{Kind: ErrTextProviderAmbiguousTimeout}, "OpenAI text request timed out after it was sent; no response was received"},
		{&TextProviderError{Kind: ErrTextProviderAmbiguousTransport}, "Connection to OpenAI was interrupted after the text request was sent; no response was received"},
		{&TextProviderError{Kind: ErrTextProviderAmbiguousTransport, StatusCode: http.StatusServiceUnavailable}, "OpenAI text API returned HTTP 503 before a result was confirmed"},
	}
	for _, tc := range tests {
		status, message := providerFailureState(tc.err)
		if status != models.AIExecutionNeedsAttention || message != tc.want {
			t.Fatalf("status=%q message=%q", status, message)
		}
		if !errors.Is(tc.err, tc.err.(*TextProviderError).Kind) {
			t.Fatalf("provider error no longer unwraps: %v", tc.err)
		}
	}
}
