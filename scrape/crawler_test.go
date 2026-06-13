package scrape

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zero/docuwarden/corpus"
)

func TestRecursiveCrawlRetriesAndReportsPartialFailure(t *testing.T) {
	var retryCount atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/docs/v1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<title>Home</title><nav><a href="/docs/v1/guide#top">Guide</a><a href="/docs/v1/guide">Duplicate</a><a href="/docs/v2">Old</a></nav><main><h1>Home</h1><p>Start</p></main>`)
	})
	mux.HandleFunc("/docs/v1/guide", func(w http.ResponseWriter, r *http.Request) {
		if retryCount.Add(1) == 1 {
			http.Error(w, "retry", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<title>Guide</title><nav><a href="/docs/v1/missing">Missing</a></nav><main><h2>Guide</h2><pre><code>go test ./...</code></pre></main>`)
	})
	mux.HandleFunc("/docs/v1/missing", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<p>no main</p>`)
	})
	mux.HandleFunc("/docs/v2", func(w http.ResponseWriter, r *http.Request) { t.Error("out-of-scope URL fetched") })
	server := httptest.NewServer(mux)
	defer server.Close()
	now := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	artifact, err := Crawl(context.Background(), Config{Source: corpus.SourceSpec{SourceID: "docs", SeedURL: server.URL + "/docs/v1", ContentSelector: "main", Version: "v1"}, Workers: 4, MaxRetries: 2, Backoff: time.Nanosecond, Now: func() time.Time { return now }, Sleep: func(context.Context, time.Duration) error { return nil }})
	if err == nil {
		t.Fatal("expected incomplete crawl error")
	}
	if artifact.Manifest.Complete {
		t.Fatal("artifact unexpectedly complete")
	}
	if len(artifact.Manifest.Documents) != 2 {
		t.Fatalf("documents = %d", len(artifact.Manifest.Documents))
	}
	if len(artifact.Report.SelectorMissing) != 1 {
		t.Fatalf("selector missing = %d", len(artifact.Report.SelectorMissing))
	}
	if len(artifact.Report.Skipped) == 0 {
		t.Fatal("expected out-of-scope skip")
	}
	if retryCount.Load() != 2 {
		t.Fatalf("guide attempts = %d", retryCount.Load())
	}
	if artifact.Markdown[artifact.Manifest.Documents[0].ID] == "" {
		t.Fatal("missing markdown")
	}
}

func TestCrawlReportsFetchingProgressAtTenPercentBuckets(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/docs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<nav>`)
		for i := 0; i < 10; i++ {
			fmt.Fprintf(w, `<a href="/docs/%d">page</a>`, i)
		}
		fmt.Fprint(w, `</nav><main>Home</main>`)
	})
	for i := 0; i < 10; i++ {
		path := fmt.Sprintf("/docs/%d", i)
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<main>Page</main>`)
		})
	}
	server := httptest.NewServer(mux)
	defer server.Close()

	var progress []string
	_, err := Crawl(context.Background(), Config{
		Source:   corpus.SourceSpec{SourceID: "docs", SeedURL: server.URL + "/docs", ContentSelector: "main"},
		Progress: func(format string, args ...any) { progress = append(progress, fmt.Sprintf(format, args...)) },
	})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(progress, "\n")
	for _, expected := range []string{"fetching 10%", "fetching 50%", "fetching 100%", "11/11 discovered pages processed"} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("progress missing %q:\n%s", expected, joined)
		}
	}
}

func TestNonHTMLFailsWithoutRetry(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{}`)
	}))
	defer server.Close()
	artifact, err := Crawl(context.Background(), Config{Source: corpus.SourceSpec{SourceID: "x", SeedURL: server.URL, ContentSelector: "main"}, MaxRetries: 3})
	if err == nil || len(artifact.Report.Failed) != 1 {
		t.Fatalf("err=%v report=%+v", err, artifact.Report)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d", calls.Load())
	}
}

func TestCrawlDiscoversNestedAnchorsWithoutContainerConfiguration(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/docs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<nav id="copied-selector"><a href="/docs/one">One</a><section><a href="/docs/two">Two</a></section></nav><main>Home</main>`)
	})
	for _, page := range []string{"one", "two"} {
		page := page
		mux.HandleFunc("/docs/"+page, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, `<main>%s</main>`, page)
		})
	}
	server := httptest.NewServer(mux)
	defer server.Close()

	artifact, err := Crawl(context.Background(), Config{Source: corpus.SourceSpec{SourceID: "docs", SeedURL: server.URL + "/docs", ContentSelector: "main"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(artifact.Manifest.Documents) != 3 {
		t.Fatalf("documents = %d, want 3", len(artifact.Manifest.Documents))
	}
}

func TestTrailingSlashRedirectPreservesRelativeLinkScope(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/en/stable", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/en/stable/", http.StatusFound)
	})
	mux.HandleFunc("/en/stable/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<nav><a href="about/introduction.html">Introduction</a></nav><main>Home</main>`)
	})
	mux.HandleFunc("/en/stable/about/introduction.html", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<main>Introduction</main>`)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	artifact, err := Crawl(context.Background(), Config{Source: corpus.SourceSpec{SourceID: "docs", SeedURL: server.URL + "/en/stable", ContentSelector: "main"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(artifact.Manifest.Documents) != 2 {
		t.Fatalf("documents = %d, want 2; report = %+v", len(artifact.Manifest.Documents), artifact.Report)
	}
	if artifact.Manifest.Documents[1].URL != server.URL+"/en/stable/about/introduction.html" {
		t.Fatalf("second document URL = %q", artifact.Manifest.Documents[1].URL)
	}
}

func TestOutOfScopeRedirectIsSkipped(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/docs/v1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<nav><a href="/docs/v1/moved">Moved</a></nav><main><h1>Home</h1></main>`)
	})
	mux.HandleFunc("/docs/v1/moved", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/docs/v2/moved", http.StatusFound)
	})
	mux.HandleFunc("/docs/v2/moved", func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("out-of-scope redirect target fetched")
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	artifact, err := Crawl(context.Background(), Config{Source: corpus.SourceSpec{SourceID: "docs", SeedURL: server.URL + "/docs/v1", ContentSelector: "main"}})
	if err != nil {
		t.Fatal(err)
	}
	if !artifact.Manifest.Complete {
		t.Fatal("artifact should be complete")
	}
	if len(artifact.Report.Failed) != 0 || len(artifact.Report.Skipped) != 1 {
		t.Fatalf("report = %+v", artifact.Report)
	}
	if artifact.Report.Skipped[0].Target != server.URL+"/docs/v2/moved" {
		t.Fatalf("skip = %+v", artifact.Report.Skipped[0])
	}
}

func TestCrawlDeduplicatesFragmentsRedirectsAndCycles(t *testing.T) {
	var rootCalls, pageCalls, redirectCalls, targetCalls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/docs", func(w http.ResponseWriter, _ *http.Request) {
		rootCalls.Add(1)
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<main>root</main><a href="/docs/page#one">page</a><a href="/docs/page#two">duplicate</a>`)
	})
	mux.HandleFunc("/docs/page", func(w http.ResponseWriter, _ *http.Request) {
		pageCalls.Add(1)
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<main>page</main><a href="/docs">cycle</a><a href="/docs/redirect">redirect</a>`)
	})
	mux.HandleFunc("/docs/redirect", func(w http.ResponseWriter, r *http.Request) {
		redirectCalls.Add(1)
		http.Redirect(w, r, "/docs/target", http.StatusFound)
	})
	mux.HandleFunc("/docs/target", func(w http.ResponseWriter, _ *http.Request) {
		targetCalls.Add(1)
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<main>target</main><a href="/docs/target#self">self</a>`)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	artifact, err := Crawl(context.Background(), Config{Source: corpus.SourceSpec{SourceID: "docs", SeedURL: server.URL + "/docs", ContentSelector: "main"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(artifact.Manifest.Documents) != 3 {
		t.Fatalf("documents=%d report=%+v", len(artifact.Manifest.Documents), artifact.Report)
	}
	if rootCalls.Load() != 1 || pageCalls.Load() != 1 || redirectCalls.Load() != 1 || targetCalls.Load() != 1 {
		t.Fatalf("calls root=%d page=%d redirect=%d target=%d", rootCalls.Load(), pageCalls.Load(), redirectCalls.Load(), targetCalls.Load())
	}
}

func TestCrawlExpandsLinksFromMissingAndConversionFailingPages(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/docs", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<main>root</main><a href="/docs/missing">missing</a><a href="/docs/conversion">conversion</a>`)
	})
	mux.HandleFunc("/docs/missing", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<section>no selected content</section><a href="/docs/from-missing">child</a>`)
	})
	mux.HandleFunc("/docs/conversion", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<main>conversion fails</main><a href="/docs/from-conversion">child</a>`)
	})
	for _, path := range []string{"/docs/from-missing", "/docs/from-conversion"} {
		mux.HandleFunc(path, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<main>discovered child</main>`)
		})
	}
	server := httptest.NewServer(mux)
	defer server.Close()

	artifact, err := Crawl(context.Background(), Config{
		Source:    corpus.SourceSpec{SourceID: "docs", SeedURL: server.URL + "/docs", ContentSelector: "main"},
		Converter: failingConverter{pageURL: server.URL + "/docs/conversion"},
	})
	if err == nil {
		t.Fatal("expected incomplete crawl")
	}
	if len(artifact.Manifest.Documents) != 3 || len(artifact.Report.SelectorMissing) != 1 || len(artifact.Report.Failed) != 1 {
		t.Fatalf("documents=%d report=%+v", len(artifact.Manifest.Documents), artifact.Report)
	}
	for _, pageURL := range []string{server.URL + "/docs/from-missing", server.URL + "/docs/from-conversion"} {
		if !hasDocumentURL(artifact, pageURL) {
			t.Fatalf("discovered page %q was not documented", pageURL)
		}
	}
}

type failingConverter struct{ pageURL string }

func (converter failingConverter) Convert(_ context.Context, pageURL string, input io.Reader) (string, error) {
	if pageURL == converter.pageURL {
		return "", errors.New("conversion failed")
	}
	body, err := io.ReadAll(input)
	return string(body), err
}

func hasDocumentURL(artifact corpus.Artifact, pageURL string) bool {
	for _, document := range artifact.Manifest.Documents {
		if document.URL == pageURL {
			return true
		}
	}
	return false
}
