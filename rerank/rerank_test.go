package rerank

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCohereContractAndDeterministicOrdering(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/rerank" {
			t.Errorf("path = %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]any{"results": []any{map[string]any{"index": 1, "relevance_score": .5}, map[string]any{"index": 0, "relevance_score": .5}}})
	}))
	defer server.Close()
	ranks, err := (Cohere{Endpoint: server.URL, Model: "latest"}).Rerank(context.Background(), "q", []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	if ranks[0].Index != 0 || ranks[1].Index != 1 {
		t.Fatalf("ranks = %+v", ranks)
	}
}

func TestCohereRejectsDuplicateIndex(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"results": []any{map[string]any{"index": 0, "relevance_score": 1}, map[string]any{"index": 0, "relevance_score": .5}}})
	}))
	defer server.Close()
	if _, err := (Cohere{Endpoint: server.URL, Model: "x"}).Rerank(context.Background(), "q", []string{"a"}); err == nil {
		t.Fatal("expected error")
	}
}

func TestVoyageContract(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/rerank" || r.Header.Get("Authorization") != "Bearer voyage-secret" {
			t.Errorf("request = %s auth=%q", r.URL.Path, r.Header.Get("Authorization"))
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request["model"] != "rerank-2.5" || request["top_k"] != float64(2) {
			t.Errorf("request = %+v", request)
		}
		if _, exists := request["top_n"]; exists {
			t.Errorf("unexpected top_n: %+v", request)
		}
		json.NewEncoder(w).Encode(map[string]any{"results": []any{map[string]any{"index": 1, "relevance_score": .9}, map[string]any{"index": 0, "relevance_score": .5}}})
	}))
	defer server.Close()

	ranks, err := (Voyage{Endpoint: server.URL, Model: "rerank-2.5", APIKey: "voyage-secret"}).Rerank(context.Background(), "q", []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	if len(ranks) != 2 || ranks[0].Index != 1 {
		t.Fatalf("ranks = %+v", ranks)
	}
}
