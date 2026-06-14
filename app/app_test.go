package app

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/zero/docuwarden/corpus"
	"github.com/zero/docuwarden/embedding"
	"github.com/zero/docuwarden/vectorstore"
)

type fakeEmbedder struct{}

func (fakeEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range out {
		out[i] = []float32{float32(i + 1), 1}
	}
	return out, nil
}

type captureEmbedder struct{ texts []string }

func (embedder *captureEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	embedder.texts = append(embedder.texts, texts...)
	out := make([][]float32, len(texts))
	for i := range out {
		out[i] = []float32{1, 2}
	}
	return out, nil
}

type fakeStore struct {
	snapshot       vectorstore.Snapshot
	cache          map[string][]float32
	profile        string
	cacheLoadCount int
}

func (store *fakeStore) LoadCachedDenseVectors(_ context.Context, _, _, profile string, _ []string) (map[string][]float32, error) {
	store.cacheLoadCount++
	if profile != store.profile {
		return nil, nil
	}
	return store.cache, nil
}

func (store *fakeStore) ReplaceSnapshot(_ context.Context, snapshot vectorstore.Snapshot) error {
	store.snapshot = snapshot
	store.profile = snapshot.EmbeddingProfile
	store.cache = map[string][]float32{}
	for _, point := range snapshot.Points {
		store.cache[point.InputHash] = append([]float32(nil), point.DenseVector...)
	}
	return nil
}
func (*fakeStore) Search(context.Context, vectorstore.SearchRequest) ([]vectorstore.Candidate, error) {
	return nil, nil
}

func TestIndexBuildsDeterministicSnapshot(t *testing.T) {
	dir := t.TempDir()
	id := corpus.DocumentID("docs", "v1", "https://example.com/docs")
	body := "# Install\n\nRun the command.\n\n## Example\n\n```sh\ngo test ./...\n```\n"
	artifact := corpus.Artifact{Manifest: corpus.Manifest{SchemaVersion: corpus.SchemaVersion, Source: corpus.SourceSpec{SourceID: "docs", DisplayName: "Example Docs", Description: "Example documentation", Tags: []string{"go"}, Version: "v1", SeedURL: "https://example.com/docs", ContentSelector: "main"}, Complete: true, Documents: []corpus.Document{{ID: id, URL: "https://example.com/docs", Title: "Docs", Filename: "documents/" + corpus.FilenameFor(id), ContentHash: corpus.HashString(body), CrawledAt: time.Unix(1, 0).UTC()}}}, Markdown: map[string]string{id: body}}
	if err := corpus.Write(dir, artifact); err != nil {
		t.Fatal(err)
	}
	store := &fakeStore{}
	service := Service{Embedder: fakeEmbedder{}, Store: store}
	if err := service.Index(context.Background(), dir, IndexOptions{EmbeddingModel: "example-embed"}); err != nil {
		t.Fatal(err)
	}
	if store.snapshot.Source != "docs" || store.snapshot.Version != "v1" || len(store.snapshot.Points) == 0 {
		t.Fatalf("snapshot = %+v", store.snapshot)
	}
	if len(store.snapshot.Documents) != 1 || store.snapshot.Documents[0].Markdown != body {
		t.Fatalf("stored documents = %#v", store.snapshot.Documents)
	}
	if len(store.snapshot.Points[0].Sparse.Indices) == 0 {
		t.Fatal("sparse vector missing")
	}
	if store.snapshot.DisplayName != "Example Docs" || store.snapshot.SeedURL != "https://example.com/docs" || store.snapshot.DocumentCount != 1 || !store.snapshot.Complete || store.snapshot.EmbeddingModel != "example-embed" {
		t.Fatalf("snapshot catalog metadata = %+v", store.snapshot)
	}
	firstID := store.snapshot.Points[0].ID
	firstDocumentID := store.snapshot.Documents[0].ID
	if err := service.Index(context.Background(), dir, IndexOptions{EmbeddingModel: "example-embed"}); err != nil {
		t.Fatal(err)
	}
	if store.snapshot.Points[0].ID != firstID {
		t.Fatal("point ID is not deterministic")
	}
	if store.snapshot.Documents[0].ID != firstDocumentID {
		t.Fatal("document point ID is not deterministic")
	}
}

func TestDocumentPointIDDependsOnSourceAndURL(t *testing.T) {
	base := vectorstore.DocumentPointID("docs", "https://example.com/page")
	if base != vectorstore.DocumentPointID("docs", "https://example.com/page") {
		t.Fatal("document point ID is not deterministic")
	}
	if base == vectorstore.DocumentPointID("other", "https://example.com/page") || base == vectorstore.DocumentPointID("docs", "https://example.com/other") {
		t.Fatal("document point IDs collided across source or URL")
	}
}

func TestIndexRejectsIncompleteByDefault(t *testing.T) {
	dir := t.TempDir()
	if err := corpus.Write(dir, corpus.Artifact{Manifest: corpus.Manifest{SchemaVersion: corpus.SchemaVersion, Source: corpus.SourceSpec{SourceID: "x"}}, Markdown: map[string]string{}}); err != nil {
		t.Fatal(err)
	}
	if err := (Service{Embedder: fakeEmbedder{}, Store: &fakeStore{}}).Index(context.Background(), dir, IndexOptions{}); err == nil {
		t.Fatal("expected error")
	}
}

func TestIndexEmbedsProvenanceWithContent(t *testing.T) {
	point := vectorstore.Point{Title: "Runtime Config", HeadingPath: []string{"API", "useRuntimeConfig"}, URL: "https://nuxt.com/docs/4.x/api/use-runtime-config", Markdown: "Call `useRuntimeConfig`.\n"}
	text := indexText(point)
	for _, expected := range []string{"Title: Runtime Config", "Headings: API > useRuntimeConfig", "URL: https://nuxt.com/", "Content:\nCall"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("missing %q in %q", expected, text)
		}
	}
}

func TestIndexReportsProgress(t *testing.T) {
	dir := t.TempDir()
	id := corpus.DocumentID("docs", "v1", "https://example.com/docs")
	body := "# Docs\n\nContent.\n"
	artifact := corpus.Artifact{Manifest: corpus.Manifest{SchemaVersion: corpus.SchemaVersion, Source: corpus.SourceSpec{SourceID: "docs", Version: "v1"}, Complete: true, Documents: []corpus.Document{{ID: id, URL: "https://example.com/docs", Filename: "documents/" + corpus.FilenameFor(id), ContentHash: corpus.HashString(body)}}}, Markdown: map[string]string{id: body}}
	if err := corpus.Write(dir, artifact); err != nil {
		t.Fatal(err)
	}
	var messages []string
	service := Service{Embedder: fakeEmbedder{}, Store: &fakeStore{}, Progress: func(format string, args ...any) {
		messages = append(messages, fmt.Sprintf(format, args...))
	}}
	if err := service.Index(context.Background(), dir, IndexOptions{BatchSize: 1}); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(messages, "\n")
	for _, expected := range []string{"index: reading artifact", "index: prepared 1 chunk(s)", "index: embedding inputs 1-1 of 1", "index: embeddings: 0 reused, 1 embedded", "index: publication complete"} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("progress missing %q:\n%s", expected, joined)
		}
	}
}

func TestIndexReusesIdenticalEmbeddingInputs(t *testing.T) {
	dir := writeIndexArtifact(t, map[string]string{
		"https://example.com/a": "# A\n\nAlpha.\n",
		"https://example.com/b": "# B\n\nBeta.\n",
	})
	store := &fakeStore{}
	first := &captureEmbedder{}
	options := IndexOptions{EmbeddingProfile: embedding.Profile{Provider: "openai", Endpoint: "https://embed.example/", Model: "model", InputType: "document"}}
	if err := (Service{Embedder: first, Store: store}).Index(context.Background(), dir, options); err != nil {
		t.Fatal(err)
	}
	if len(first.texts) != 2 {
		t.Fatalf("first embedded %d inputs", len(first.texts))
	}
	second := &captureEmbedder{}
	if err := (Service{Embedder: second, Store: store}).Index(context.Background(), dir, options); err != nil {
		t.Fatal(err)
	}
	if len(second.texts) != 0 {
		t.Fatalf("second embedded %d inputs", len(second.texts))
	}
}

func TestIndexEmbedsOnlyChangedInputs(t *testing.T) {
	dir := writeIndexArtifact(t, map[string]string{
		"https://example.com/a": "# A\n\nAlpha.\n",
		"https://example.com/b": "# B\n\nBeta.\n",
	})
	store := &fakeStore{}
	options := IndexOptions{EmbeddingProfile: embedding.Profile{Provider: "openai", Endpoint: "https://embed.example", Model: "model", InputType: "document"}}
	if err := (Service{Embedder: &captureEmbedder{}, Store: store}).Index(context.Background(), dir, options); err != nil {
		t.Fatal(err)
	}
	dir = writeIndexArtifact(t, map[string]string{
		"https://example.com/a": "# A\n\nAlpha changed.\n",
		"https://example.com/b": "# B\n\nBeta.\n",
	})
	embedder := &captureEmbedder{}
	if err := (Service{Embedder: embedder, Store: store}).Index(context.Background(), dir, options); err != nil {
		t.Fatal(err)
	}
	if len(embedder.texts) != 1 || !strings.Contains(embedder.texts[0], "Alpha changed") {
		t.Fatalf("embedded inputs = %#v", embedder.texts)
	}
}

func TestIndexProfileChangeAndForceReembedBypassCache(t *testing.T) {
	dir := writeIndexArtifact(t, map[string]string{"https://example.com/a": "# A\n\nAlpha.\n"})
	store := &fakeStore{}
	base := IndexOptions{EmbeddingProfile: embedding.Profile{Provider: "openai", Endpoint: "https://embed.example", Model: "model", InputType: "document"}}
	if err := (Service{Embedder: &captureEmbedder{}, Store: store}).Index(context.Background(), dir, base); err != nil {
		t.Fatal(err)
	}
	changed := base
	changed.EmbeddingProfile.Model = "model-2"
	embedder := &captureEmbedder{}
	if err := (Service{Embedder: embedder, Store: store}).Index(context.Background(), dir, changed); err != nil {
		t.Fatal(err)
	}
	if len(embedder.texts) != 1 {
		t.Fatalf("profile change embedded %d inputs", len(embedder.texts))
	}
	forced := changed
	forced.ForceReembed = true
	embedder = &captureEmbedder{}
	loads := store.cacheLoadCount
	if err := (Service{Embedder: embedder, Store: store}).Index(context.Background(), dir, forced); err != nil {
		t.Fatal(err)
	}
	if len(embedder.texts) != 1 || store.cacheLoadCount != loads {
		t.Fatalf("force embedded %d inputs and cache loads changed from %d to %d", len(embedder.texts), loads, store.cacheLoadCount)
	}
}

func writeIndexArtifact(t *testing.T, pages map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	artifact := corpus.Artifact{Manifest: corpus.Manifest{SchemaVersion: corpus.SchemaVersion, Source: corpus.SourceSpec{SourceID: "docs", Version: "v1"}, Complete: true}, Markdown: map[string]string{}}
	for url, body := range pages {
		id := corpus.DocumentID("docs", "v1", url)
		artifact.Manifest.Documents = append(artifact.Manifest.Documents, corpus.Document{ID: id, URL: url, Title: url, Filename: "documents/" + corpus.FilenameFor(id), ContentHash: corpus.HashString(body)})
		artifact.Markdown[id] = body
	}
	corpus.Sort(&artifact)
	if err := corpus.Write(dir, artifact); err != nil {
		t.Fatal(err)
	}
	return dir
}
