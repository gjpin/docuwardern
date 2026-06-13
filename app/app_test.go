package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/zero/docuwarden/corpus"
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

type fakeStore struct{ snapshot vectorstore.Snapshot }

func (store *fakeStore) ReplaceSnapshot(_ context.Context, snapshot vectorstore.Snapshot) error {
	store.snapshot = snapshot
	return nil
}
func (*fakeStore) Search(context.Context, vectorstore.SearchRequest) ([]vectorstore.Candidate, error) {
	return nil, nil
}

func TestIndexBuildsDeterministicSnapshot(t *testing.T) {
	dir := t.TempDir()
	id := corpus.DocumentID("docs", "v1", "https://example.com/docs")
	body := "# Install\n\nRun the command.\n\n## Example\n\n```sh\ngo test ./...\n```\n"
	artifact := corpus.Artifact{Manifest: corpus.Manifest{SchemaVersion: corpus.SchemaVersion, Source: corpus.SourceSpec{SourceID: "docs", Version: "v1", SeedURL: "https://example.com/docs", ContentSelector: "main"}, Complete: true, Documents: []corpus.Document{{ID: id, URL: "https://example.com/docs", Title: "Docs", Filename: "documents/" + corpus.FilenameFor(id), ContentHash: corpus.HashString(body), CrawledAt: time.Unix(1, 0).UTC()}}}, Markdown: map[string]string{id: body}}
	if err := corpus.Write(dir, artifact); err != nil {
		t.Fatal(err)
	}
	store := &fakeStore{}
	service := Service{Embedder: fakeEmbedder{}, Store: store}
	if err := service.Index(context.Background(), dir, IndexOptions{}); err != nil {
		t.Fatal(err)
	}
	if store.snapshot.Source != "docs" || store.snapshot.Version != "v1" || len(store.snapshot.Points) == 0 {
		t.Fatalf("snapshot = %+v", store.snapshot)
	}
	if len(store.snapshot.Points[0].Sparse.Indices) == 0 {
		t.Fatal("sparse vector missing")
	}
	firstID := store.snapshot.Points[0].ID
	if err := service.Index(context.Background(), dir, IndexOptions{}); err != nil {
		t.Fatal(err)
	}
	if store.snapshot.Points[0].ID != firstID {
		t.Fatal("point ID is not deterministic")
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
