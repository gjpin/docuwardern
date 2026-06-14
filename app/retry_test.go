package app

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zero/docuwarden/corpus"
	"github.com/zero/docuwarden/scrape"
)

func TestRetryRepairsAndExtendsIncompleteArtifact(t *testing.T) {
	var repaired atomic.Bool
	var goodCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		switch r.URL.Path {
		case "/docs":
			fmt.Fprint(w, `<main>root</main><nav><a href="/docs/good">good</a><a href="/docs/broken">broken</a><a href="/docs/missing">missing</a></nav>`)
		case "/docs/good":
			goodCalls.Add(1)
			fmt.Fprint(w, `<main>good</main>`)
		case "/docs/broken":
			if !repaired.Load() {
				http.Error(w, "broken", http.StatusServiceUnavailable)
				return
			}
			fmt.Fprint(w, `<main>original wins</main><article>fallback</article><div class="extra"><a href="/docs/new">new</a></div>`)
		case "/docs/missing":
			fmt.Fprint(w, `<article>repaired missing</article>`)
		case "/docs/new":
			fmt.Fprint(w, `<main>new page</main>`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	dir := filepath.Join(t.TempDir(), "artifact")
	cfg := scrape.Config{Source: corpus.SourceSpec{SourceID: "docs", SeedURL: server.URL + "/docs", ContentSelector: "main"}, Workers: 2, MaxRetries: 0, Backoff: time.Nanosecond}
	initial, err := scrape.Crawl(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected incomplete initial crawl")
	}
	if err := corpus.Write(dir, initial); err != nil {
		t.Fatal(err)
	}
	goodID := corpus.DocumentID("docs", "", server.URL+"/docs/good")
	goodCrawledAt := documentByID(t, initial, goodID).CrawledAt

	repaired.Store(true)
	result, err := Retry(context.Background(), dir, RetryOptions{ContentSelectors: []string{"main", "article", "article"}})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Manifest.Complete || len(result.Manifest.Documents) != 5 {
		t.Fatalf("complete=%v documents=%d report=%+v", result.Manifest.Complete, len(result.Manifest.Documents), result.Report)
	}
	if goodCalls.Load() != 1 {
		t.Fatalf("successful page fetched %d times", goodCalls.Load())
	}
	if documentByID(t, result, goodID).CrawledAt != goodCrawledAt {
		t.Fatal("existing successful document was replaced")
	}
	if got := result.Manifest.Source.ContentSelectors; len(got) != 1 || got[0] != "article" {
		t.Fatalf("content selectors = %#v", got)
	}
	brokenID := corpus.DocumentID("docs", "", server.URL+"/docs/broken")
	if body := result.Markdown[brokenID]; body == "" || contains(body, "fallback") || !contains(body, "original wins") {
		t.Fatalf("primary selector did not win: %q", body)
	}
	if _, err := corpus.Read(dir); err != nil {
		t.Fatalf("read merged artifact: %v", err)
	}
}

func TestRetryRetainsFailureAndReturnsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "still broken", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	dir := filepath.Join(t.TempDir(), "artifact")
	artifact := incompleteArtifact(server.URL, corpus.Report{Failed: []corpus.PageEvent{{URL: server.URL, StatusCode: http.StatusServiceUnavailable, Detail: "old"}}})
	if err := corpus.Write(dir, artifact); err != nil {
		t.Fatal(err)
	}
	result, err := Retry(context.Background(), dir, RetryOptions{MaxRetries: 0, MaxRetriesSet: true})
	if err == nil {
		t.Fatal("expected retry error")
	}
	if result.Manifest.Complete || len(result.Report.Failed) != 1 || result.Report.Failed[0].Detail == "old" {
		t.Fatalf("result = %+v", result.Report)
	}
}

func TestRetryRedirectToExistingDocumentDoesNotDuplicate(t *testing.T) {
	server := httptest.NewServer(nil)
	server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/docs/retry" {
			http.Redirect(w, r, "/docs/good", http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<main>good</main>`)
	})
	defer server.Close()
	body := "good"
	id := corpus.DocumentID("docs", "", server.URL+"/docs/good")
	artifact := incompleteArtifact(server.URL+"/docs", corpus.Report{Failed: []corpus.PageEvent{{URL: server.URL + "/docs/retry"}}})
	artifact.Manifest.Documents = []corpus.Document{{ID: id, URL: server.URL + "/docs/good", Filename: "documents/" + corpus.FilenameFor(id), ContentHash: corpus.HashString(body)}}
	artifact.Markdown[id] = body
	dir := filepath.Join(t.TempDir(), "artifact")
	if err := corpus.Write(dir, artifact); err != nil {
		t.Fatal(err)
	}
	result, err := Retry(context.Background(), dir, RetryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Manifest.Documents) != 1 || !result.Manifest.Complete {
		t.Fatalf("documents=%d complete=%v report=%+v", len(result.Manifest.Documents), result.Manifest.Complete, result.Report)
	}
}

func TestRetryMetaRefreshToExistingDocumentReplacesSelectorMissing(t *testing.T) {
	var targetCalls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/docs/redirect", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<link rel="canonical" href="/docs/good"><meta http-equiv="refresh" content="0; url=/docs/good">`)
	})
	mux.HandleFunc("/docs/good", func(w http.ResponseWriter, _ *http.Request) {
		targetCalls.Add(1)
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<main>good</main>`)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	body := "good"
	targetURL := server.URL + "/docs/good"
	id := corpus.DocumentID("docs", "", targetURL)
	artifact := incompleteArtifact(server.URL+"/docs", corpus.Report{SelectorMissing: []corpus.PageEvent{{URL: server.URL + "/docs/redirect", StatusCode: http.StatusOK, Detail: "content selector did not match"}}})
	artifact.Manifest.Documents = []corpus.Document{{ID: id, URL: targetURL, Filename: "documents/" + corpus.FilenameFor(id), ContentHash: corpus.HashString(body)}}
	artifact.Markdown[id] = body
	dir := filepath.Join(t.TempDir(), "artifact")
	if err := corpus.Write(dir, artifact); err != nil {
		t.Fatal(err)
	}

	result, err := Retry(context.Background(), dir, RetryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if targetCalls.Load() != 0 || len(result.Manifest.Documents) != 1 || len(result.Report.SelectorMissing) != 0 || len(result.Report.Redirected) != 1 || !result.Manifest.Complete {
		t.Fatalf("target calls=%d documents=%d complete=%v report=%+v", targetCalls.Load(), len(result.Manifest.Documents), result.Manifest.Complete, result.Report)
	}
	event := result.Report.Redirected[0]
	if event.URL != server.URL+"/docs/redirect" || event.Target != targetURL || event.Detail != "HTML meta refresh" {
		t.Fatalf("redirect=%+v", event)
	}
}

func TestRetryCompleteArtifactIsNoOp(t *testing.T) {
	artifact := incompleteArtifact("https://example.com/docs", corpus.Report{})
	artifact.Manifest.Complete = true
	dir := filepath.Join(t.TempDir(), "artifact")
	if err := corpus.Write(dir, artifact); err != nil {
		t.Fatal(err)
	}
	result, err := Retry(context.Background(), dir, RetryOptions{ContentSelectors: []string{"["}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Manifest.Source.ContentSelectors) != 0 {
		t.Fatalf("complete artifact changed: %+v", result.Manifest.Source)
	}
}

func incompleteArtifact(seed string, report corpus.Report) corpus.Artifact {
	return corpus.Artifact{Manifest: corpus.Manifest{SchemaVersion: corpus.SchemaVersion, Source: corpus.SourceSpec{SourceID: "docs", SeedURL: seed, ContentSelector: "main"}, Crawl: corpus.CrawlSettings{Workers: 1, Timeout: time.Second, Backoff: time.Nanosecond}}, Report: report, Markdown: map[string]string{}}
}

func documentByID(t *testing.T, artifact corpus.Artifact, id string) corpus.Document {
	t.Helper()
	for _, document := range artifact.Manifest.Documents {
		if document.ID == id {
			return document
		}
	}
	t.Fatalf("document %s not found", id)
	return corpus.Document{}
}

func contains(value, substring string) bool {
	for i := 0; i+len(substring) <= len(value); i++ {
		if value[i:i+len(substring)] == substring {
			return true
		}
	}
	return false
}
