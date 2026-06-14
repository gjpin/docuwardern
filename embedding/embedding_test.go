package embedding

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenAIContractAndOrdering(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" || r.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("request = %s auth=%q", r.URL.Path, r.Header.Get("Authorization"))
		}
		var request struct {
			Model string   `json:"model"`
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.Model != "latest" || len(request.Input) != 2 {
			t.Errorf("request = %+v", request)
		}
		json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"index": 1, "embedding": []float32{3, 4}}, map[string]any{"index": 0, "embedding": []float32{1, 2}}}})
	}))
	defer server.Close()
	vectors, err := (OpenAI{Endpoint: server.URL, Model: "latest", APIKey: "secret"}).Embed(context.Background(), []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	if vectors[0][0] != 1 || vectors[1][0] != 3 {
		t.Fatalf("vectors = %v", vectors)
	}
}

func TestOpenAIRejectsMalformedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"data": []any{}})
	}))
	defer server.Close()
	if _, err := (OpenAI{Endpoint: server.URL, Model: "x"}).Embed(context.Background(), []string{"a"}); err == nil {
		t.Fatal("expected error")
	}
}

func TestVoyageContract(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" || r.Header.Get("Authorization") != "Bearer voyage-secret" {
			t.Errorf("request = %s auth=%q", r.URL.Path, r.Header.Get("Authorization"))
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request["model"] != "voyage-4-large" || request["input_type"] != "document" {
			t.Errorf("request = %+v", request)
		}
		if _, exists := request["encoding_format"]; exists {
			t.Errorf("unexpected encoding_format: %+v", request)
		}
		json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"index": 0, "embedding": []float32{1, 2}}}})
	}))
	defer server.Close()

	vectors, err := (Voyage{Endpoint: server.URL, Model: "voyage-4-large", APIKey: "voyage-secret", InputType: "document"}).Embed(context.Background(), []string{"a"})
	if err != nil {
		t.Fatal(err)
	}
	if len(vectors) != 1 || vectors[0][0] != 1 {
		t.Fatalf("vectors = %v", vectors)
	}
}

func TestProfileFingerprintNormalizesEndpointAndCoversCompatibilityFields(t *testing.T) {
	base := Profile{Provider: "OpenAI", Endpoint: "HTTPS://EMBED.EXAMPLE/api/", Model: "model", InputType: "document", InputFormatVersion: 1}
	normalized := base
	normalized.Provider = "openai"
	normalized.Endpoint = "https://embed.example/api"
	if base.Fingerprint() != normalized.Fingerprint() {
		t.Fatal("equivalent endpoints produced different fingerprints")
	}
	changes := []Profile{
		{Provider: "voyage", Endpoint: normalized.Endpoint, Model: normalized.Model, InputType: normalized.InputType, InputFormatVersion: 1},
		{Provider: normalized.Provider, Endpoint: "https://other.example/api", Model: normalized.Model, InputType: normalized.InputType, InputFormatVersion: 1},
		{Provider: normalized.Provider, Endpoint: normalized.Endpoint, Model: "model-2", InputType: normalized.InputType, InputFormatVersion: 1},
		{Provider: normalized.Provider, Endpoint: normalized.Endpoint, Model: normalized.Model, InputType: "query", InputFormatVersion: 1},
		{Provider: normalized.Provider, Endpoint: normalized.Endpoint, Model: normalized.Model, InputType: normalized.InputType, InputFormatVersion: 2},
	}
	for i, changed := range changes {
		if changed.Fingerprint() == normalized.Fingerprint() {
			t.Fatalf("compatibility change %d did not alter fingerprint", i)
		}
	}
}
