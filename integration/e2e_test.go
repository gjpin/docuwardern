//go:build integration

package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestCompiledCLIWorkflow(t *testing.T) {
	var broken atomic.Bool
	var modelBroken atomic.Bool
	docs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		switch r.URL.Path {
		case "/docs/v1":
			fmt.Fprint(w, `<title>V1 Home</title><nav><a href="/docs/v1/install">Install</a><a href="/docs/v1/install#dup">Duplicate</a><a href="/outside">Outside</a></nav><main><h1>Version One</h1><p>Legacy overview.</p></main>`)
		case "/docs/v1/install":
			fmt.Fprint(w, `<title>V1 Install</title><main><h1>Install</h1><p>Use the legacy install command.</p><table><tr><th>OS</th><th>Command</th></tr><tr><td>Linux</td><td>old-install</td></tr></table><pre><code>old-install --stable</code></pre></main>`)
		case "/docs/v2":
			fmt.Fprint(w, `<title>V2 Home</title><nav><a href="/docs/v2/install">Install</a><a href="/docs/v2/runtime">Runtime</a></nav><main><h1>Version Two</h1><p>Current overview.</p></main>`)
		case "/docs/v2/install":
			body := "Use the current fast install command."
			if broken.Load() {
				body = "FAIL_EMBED"
			}
			fmt.Fprintf(w, `<title>V2 Install</title><main><h1>Install</h1><p>%s</p><pre><code>new-install --fast</code></pre></main>`, body)
		case "/docs/v2/runtime":
			fmt.Fprint(w, `<title>Runtime Config</title><main><h1>Runtime Configuration</h1><p>Call useRuntimeConfig to access runtime values.</p></main>`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer docs.Close()
	models := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if modelBroken.Load() && r.URL.Path == "/v1/embeddings" {
			http.Error(w, "failure", http.StatusInternalServerError)
			return
		}
		fakeModels(w, r)
	}))
	defer models.Close()
	root := filepath.Clean("..")
	binary := filepath.Join(t.TempDir(), "docuwarden")
	build := exec.Command("go", "build", "-o", binary, "./cmd/docuwarden")
	build.Dir = root
	build.Env = append(os.Environ(), "GOCACHE=/tmp/docuwarden-go-cache")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, output)
	}
	source := fmt.Sprintf("e2e-%d", os.Getpid())
	base := []string{"--embedding-endpoint", models.URL, "--embedding-model", "fake", "--reranker-endpoint", models.URL, "--reranker-model", "fake", "--qdrant-host", env("DOCUWARDEN_QDRANT_HOST", "localhost"), "--qdrant-port", env("DOCUWARDEN_QDRANT_PORT", "6334")}
	artifact1 := filepath.Join(t.TempDir(), "v1")
	run(t, binary, append(base, "ingest", docs.URL+"/docs/v1", "--source", source, "--version", "v1", "--content-selector", "main", "--output", artifact1, "--throttle", "0")...)
	artifact2 := filepath.Join(t.TempDir(), "v2")
	run(t, binary, append(base, "ingest", docs.URL+"/docs/v2", "--source", source, "--version", "v2", "--content-selector", "main", "--output", artifact2, "--throttle", "0")...)
	sourcesJSON := run(t, binary, append(base, "sources", "--format", "json")...)
	if !strings.Contains(sourcesJSON, `"source": "`+source+`"`) || !strings.Contains(sourcesJSON, `"default_version": "v2"`) || !strings.Contains(sourcesJSON, `"document_count": 3`) {
		t.Fatalf("sources catalog:\n%s", sourcesJSON)
	}
	documentsJSON := run(t, binary, append(base, "documents", "--source", source, "--version", "v2", "--format", "json")...)
	if !strings.Contains(documentsJSON, `"version": "v2"`) || !strings.Contains(documentsJSON, docs.URL+"/docs/v2/runtime") {
		t.Fatalf("documents catalog:\n%s", documentsJSON)
	}
	defaultJSON := run(t, binary, append(base, "search", "current fast install", "--source", source, "--format", "json")...)
	if !strings.Contains(defaultJSON, `"version": "v2"`) || !strings.Contains(defaultJSON, "new-install") {
		t.Fatalf("default search:\n%s", defaultJSON)
	}
	exactJSON := run(t, binary, append(base, "search", "useRuntimeConfig", "--source", source, "--format", "json")...)
	if !strings.Contains(exactJSON, "useRuntimeConfig") || !strings.Contains(exactJSON, `"sparse_score":`) {
		t.Fatalf("exact identifier search:\n%s", exactJSON)
	}
	v1JSON := run(t, binary, append(base, "search", "legacy install", "--source", source, "--version", "v1", "--format", "json")...)
	if !strings.Contains(v1JSON, `"version": "v1"`) || strings.Contains(v1JSON, "new-install") {
		t.Fatalf("explicit search:\n%s", v1JSON)
	}
	text := run(t, binary, append(base, "search", "current fast install", "--source", source, "--format", "text")...)
	if !strings.Contains(text, "Source:") || !strings.Contains(text, "```") {
		t.Fatalf("text search:\n%s", text)
	}
	broken.Store(true)
	modelBroken.Store(true)
	failed := exec.Command(binary, append(base, "ingest", docs.URL+"/docs/v2", "--source", source, "--version", "v2", "--content-selector", "main", "--output", filepath.Join(t.TempDir(), "broken"), "--throttle", "0")...)
	if output, err := failed.CombinedOutput(); err == nil {
		t.Fatalf("broken replacement succeeded:\n%s", output)
	}
	modelBroken.Store(false)
	afterFailure := run(t, binary, append(base, "search", "current fast install", "--source", source, "--format", "json")...)
	if !strings.Contains(afterFailure, "new-install") {
		t.Fatalf("old alias was not preserved:\n%s", afterFailure)
	}
}

func fakeModels(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.URL.Path == "/v1/embeddings" {
		var request struct {
			Input []string `json:"input"`
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		data := make([]any, len(request.Input))
		for i, text := range request.Input {
			if strings.Contains(text, "FAIL_EMBED") {
				http.Error(w, "failure", 500)
				return
			}
			data[i] = map[string]any{"index": i, "embedding": vector(text)}
		}
		json.NewEncoder(w).Encode(map[string]any{"data": data})
		return
	}
	if r.URL.Path == "/v1/rerank" {
		var request struct {
			Query     string   `json:"query"`
			Documents []string `json:"documents"`
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		results := make([]any, len(request.Documents))
		for i, document := range request.Documents {
			score := .1
			for _, word := range strings.Fields(strings.ToLower(request.Query)) {
				if strings.Contains(strings.ToLower(document), word) {
					score += 1
				}
			}
			results[i] = map[string]any{"index": i, "relevance_score": score}
		}
		json.NewEncoder(w).Encode(map[string]any{"results": results})
		return
	}
	http.NotFound(w, r)
}

func vector(text string) []float32 {
	lower := strings.ToLower(text)
	return []float32{boolFloat(strings.Contains(lower, "install")), boolFloat(strings.Contains(lower, "current") || strings.Contains(lower, "fast") || strings.Contains(lower, "new-")), boolFloat(strings.Contains(lower, "legacy") || strings.Contains(lower, "old-")), 1}
}
func boolFloat(value bool) float32 {
	if value {
		return 1
	}
	return 0
}
func run(t *testing.T, binary string, args ...string) string {
	t.Helper()
	command := exec.Command(binary, args...)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		t.Fatalf("%s: %v\nstderr: %s", strings.Join(args, " "), err, stderr.String())
	}
	return stdout.String()
}
func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
