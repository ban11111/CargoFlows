package ai

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPProviderVerifierAuthenticatesWithBearerKey(t *testing.T) {
	const apiKey = "sk-proj-verifier-secret-ABCD"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Errorf("path = %q, want /models", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+apiKey {
			t.Errorf("authorization = %q", got)
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	result, err := NewHTTPProviderVerifier(server.URL+"/", server.Client()).Verify(t.Context(), apiKey)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Authenticated {
		t.Fatalf("result = %#v", result)
	}
}

func TestHTTPProviderVerifierMapsSafeProviderErrors(t *testing.T) {
	const apiKey = "sk-proj-verifier-secret-WXYZ"
	tests := []struct {
		name       string
		status     int
		credential bool
	}{
		{name: "unauthorized", status: http.StatusUnauthorized, credential: true},
		{name: "forbidden", status: http.StatusForbidden, credential: true},
		{name: "server error", status: http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte("response body contains " + apiKey))
			}))
			t.Cleanup(server.Close)

			_, err := NewHTTPProviderVerifier(server.URL, server.Client()).Verify(t.Context(), apiKey)
			if err == nil {
				t.Fatal("expected error")
			}
			if tt.credential != errors.Is(err, ErrCredentialVerification) {
				t.Fatalf("credential error = %v, want %v", errors.Is(err, ErrCredentialVerification), tt.credential)
			}
			if strings.Contains(err.Error(), apiKey) {
				t.Fatalf("error leaked key: %v", err)
			}
		})
	}
}

type leakingRoundTripper struct {
	apiKey string
}

func (t leakingRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("transport rejected " + t.apiKey)
}

func TestHTTPProviderVerifierSanitizesTransportErrors(t *testing.T) {
	const apiKey = "sk-proj-transport-secret-ABCD"
	client := &http.Client{Transport: leakingRoundTripper{apiKey: apiKey}}
	_, err := NewHTTPProviderVerifier("https://api.openai.invalid/v1", client).Verify(t.Context(), apiKey)
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), apiKey) {
		t.Fatalf("transport error leaked key: %v", err)
	}
}
