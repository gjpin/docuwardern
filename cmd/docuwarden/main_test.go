package main

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/zero/docuwarden/embedding"
	"github.com/zero/docuwarden/rerank"
)

func TestScrapeCommandCreatesArtifact(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<title>Docs</title><main><h1>Hello</h1><a href="next">next</a></main>`)
	}))
	defer server.Close()
	dir := filepath.Join(t.TempDir(), "artifact")
	var stdout, stderr bytes.Buffer
	command := newRoot(&stdout, &stderr)
	command.SetArgs([]string{"scrape", server.URL, "--source", "test", "--content-selector", "main", "--output", dir, "--throttle", "0"})
	if err := command.Execute(); err != nil {
		t.Fatalf("execute: %v stderr=%s", err, stderr.String())
	}
	for _, name := range []string{"manifest.json", "report.json", "documents"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("missing %s: %v", name, err)
		}
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout contaminated: %q", stdout.String())
	}
}

func TestSearchRequiresSourceBeforeConnecting(t *testing.T) {
	command := newRoot(&bytes.Buffer{}, &bytes.Buffer{})
	command.SetArgs([]string{"search", "query"})
	if err := command.Execute(); err == nil {
		t.Fatal("expected error")
	}
}

func TestVoyageProviderFactories(t *testing.T) {
	t.Setenv("VOYAGE_API_KEY", "voyage-key")
	t.Setenv("DOCUWARDEN_EMBEDDING_API_KEY", "embedding-key")
	flags := providerFlags{
		embeddingProvider: "voyage",
		embeddingModel:    "voyage-4-large",
		rerankerProvider:  "voyage",
		rerankerModel:     "rerank-2.5",
	}

	indexEmbedder, err := flags.embedder(http.DefaultClient, "document")
	if err != nil {
		t.Fatal(err)
	}
	indexVoyage, ok := indexEmbedder.(embedding.Voyage)
	if !ok || indexVoyage.Endpoint != "https://api.voyageai.com" || indexVoyage.InputType != "document" || indexVoyage.APIKey != "embedding-key" {
		t.Fatalf("index embedder = %#v", indexEmbedder)
	}

	queryEmbedder, err := flags.embedder(http.DefaultClient, "query")
	if err != nil {
		t.Fatal(err)
	}
	queryVoyage := queryEmbedder.(embedding.Voyage)
	if queryVoyage.InputType != "query" {
		t.Fatalf("query input type = %q", queryVoyage.InputType)
	}

	ranker, err := flags.reranker(http.DefaultClient)
	if err != nil {
		t.Fatal(err)
	}
	voyageRanker, ok := ranker.(rerank.Voyage)
	if !ok || voyageRanker.Endpoint != "https://api.voyageai.com" || voyageRanker.APIKey != "voyage-key" {
		t.Fatalf("reranker = %#v", ranker)
	}
}

func TestProviderFactoriesValidateConfiguration(t *testing.T) {
	if _, err := (providerFlags{embeddingProvider: "unknown", embeddingModel: "model"}).embedder(http.DefaultClient, "query"); err == nil {
		t.Fatal("expected invalid embedding provider error")
	}
	if _, err := (providerFlags{embeddingProvider: "openai", embeddingModel: "model"}).embedder(http.DefaultClient, "query"); err == nil {
		t.Fatal("expected missing OpenAI endpoint error")
	}
	if _, err := (providerFlags{rerankerProvider: "unknown", rerankerModel: "model"}).reranker(http.DefaultClient); err == nil {
		t.Fatal("expected invalid reranker provider error")
	}
	if _, err := (providerFlags{rerankerProvider: "cohere", rerankerModel: "model"}).reranker(http.DefaultClient); err == nil {
		t.Fatal("expected missing Cohere endpoint error")
	}
}
