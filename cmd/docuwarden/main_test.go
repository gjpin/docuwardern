package main

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zero/docuwarden/corpus"
	"github.com/zero/docuwarden/embedding"
	"github.com/zero/docuwarden/rerank"
	"github.com/zero/docuwarden/vectorstore"
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
	for _, expected := range []string{"crawl: starting", "crawl: fetching", "artifact: written"} {
		if !strings.Contains(stderr.String(), expected) {
			t.Fatalf("stderr missing %q: %s", expected, stderr.String())
		}
	}
}

func TestRetryCommandUsesRepeatedSelectorsAndOverrides(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<article>repaired</article>`)
	}))
	defer server.Close()
	dir := filepath.Join(t.TempDir(), "artifact")
	artifact := corpus.Artifact{Manifest: corpus.Manifest{SchemaVersion: corpus.SchemaVersion, Source: corpus.SourceSpec{SourceID: "docs", SeedURL: server.URL, ContentSelector: "main"}, Crawl: corpus.CrawlSettings{Workers: 1, Timeout: time.Second, Backoff: time.Millisecond}}, Report: corpus.Report{SelectorMissing: []corpus.PageEvent{{URL: server.URL}}}, Markdown: map[string]string{}}
	if err := corpus.Write(dir, artifact); err != nil {
		t.Fatal(err)
	}
	command := newRoot(&bytes.Buffer{}, &bytes.Buffer{})
	command.SetArgs([]string{"retry", dir, "--content-selector", "article", "--content-selector", "article", "--link-selector", ".links", "--link-selector", ".links", "--workers", "2", "--retries", "0", "--throttle", "0"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	result, err := corpus.Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Manifest.Complete || result.Manifest.Crawl.Workers != 2 || result.Manifest.Crawl.MaxRetries != 0 || result.Manifest.Crawl.Throttle != 0 {
		t.Fatalf("manifest = %+v", result.Manifest)
	}
	if len(result.Manifest.Source.ContentSelectors) != 1 || len(result.Manifest.Source.LinkSelectors) != 1 {
		t.Fatalf("source = %+v", result.Manifest.Source)
	}
}

func TestRetryCommandRejectsInvalidArtifact(t *testing.T) {
	command := newRoot(&bytes.Buffer{}, &bytes.Buffer{})
	command.SetArgs([]string{"retry", t.TempDir()})
	if err := command.Execute(); err == nil {
		t.Fatal("expected invalid artifact error")
	}
}

func TestSearchRequiresSourceBeforeConnecting(t *testing.T) {
	command := newRoot(&bytes.Buffer{}, &bytes.Buffer{})
	command.SetArgs([]string{"search", "query"})
	if err := command.Execute(); err == nil {
		t.Fatal("expected error")
	}
}

func TestCatalogJSONOutput(t *testing.T) {
	var output bytes.Buffer
	catalog := vectorstore.Catalog{SchemaVersion: 1, Sources: []vectorstore.CatalogSource{{Source: "nuxt", DisplayName: "Nuxt", DefaultVersion: "4.x", Versions: []vectorstore.CatalogVersion{{Version: "4.x", DocumentCount: 2, ChunkCount: 7, Complete: true, IndexedAt: "1970-01-01T00:00:01Z"}}}}}
	if err := writeCatalog(&output, catalog, "json"); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`"schema_version": 1`, `"source": "nuxt"`, `"default_version": "4.x"`, `"chunk_count": 7`} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("catalog output missing %s:\n%s", expected, output.String())
		}
	}
}

func TestDocumentsTextOutput(t *testing.T) {
	var output bytes.Buffer
	documents := vectorstore.DocumentCatalog{SchemaVersion: 1, Source: "nuxt", Version: "4.x", Documents: []vectorstore.CatalogDocument{{URL: "https://nuxt.com/docs/4.x/guide", Title: "Guide"}}}
	if err := writeDocuments(&output, documents, "text"); err != nil {
		t.Fatal(err)
	}
	if output.String() != "Guide\n  https://nuxt.com/docs/4.x/guide\n" {
		t.Fatalf("documents output = %q", output.String())
	}
}

func TestDocumentsRequiresSourceBeforeConnecting(t *testing.T) {
	command := newRoot(&bytes.Buffer{}, &bytes.Buffer{})
	command.SetArgs([]string{"documents"})
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
