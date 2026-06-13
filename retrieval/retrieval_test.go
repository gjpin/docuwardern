package retrieval

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/zero/docuwarden/rerank"
	"github.com/zero/docuwarden/vectorstore"
)

type embedderStub struct{}

func (embedderStub) Embed(context.Context, []string) ([][]float32, error) {
	return [][]float32{{1, 2}}, nil
}

type rerankerStub struct{}

func (rerankerStub) Rerank(context.Context, string, []string) ([]rerank.Rank, error) {
	return []rerank.Rank{{Index: 1, Score: .9}, {Index: 0, Score: .5}}, nil
}

type storeStub struct{ limit int }

func (*storeStub) ReplaceSnapshot(context.Context, vectorstore.Snapshot) error { return nil }
func (store *storeStub) Search(_ context.Context, request vectorstore.SearchRequest) ([]vectorstore.Candidate, error) {
	store.limit = request.Limit
	return []vectorstore.Candidate{{Point: vectorstore.Point{ID: "a", Source: request.Source, Version: request.Version, URL: "https://x/a", Title: "A", Markdown: "A body"}, DenseScore: .8, FusionScore: .02}, {Point: vectorstore.Point{ID: "b", Source: request.Source, Version: request.Version, URL: "https://x/b", Title: "B", HeadingPath: []string{"API", "Run"}, Markdown: "```go\nrun()\n```"}, DenseScore: .7, SparseScore: 2.1, FusionScore: .03}}, nil
}

func TestSearchReranksAndFormats(t *testing.T) {
	store := &storeStub{}
	service := Service{Embedder: embedderStub{}, Reranker: rerankerStub{}, Store: store}
	results, err := service.Search(context.Background(), "run", "docs", "v1", 1)
	if err != nil {
		t.Fatal(err)
	}
	if store.limit != 40 || len(results) != 1 || results[0].URL != "https://x/b" || results[0].VectorScore != .03 || results[0].SparseScore != 2.1 {
		t.Fatalf("results=%+v candidateLimit=%d", results, store.limit)
	}
	var jsonOut bytes.Buffer
	if err := WriteJSON(&jsonOut, results); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(jsonOut.String(), `"reranker_score": 0.9`) {
		t.Fatalf("json = %s", jsonOut.String())
	}
	var textOut bytes.Buffer
	if err := WriteText(&textOut, results); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(textOut.String(), "Heading: API > Run") || !strings.Contains(textOut.String(), "```go") {
		t.Fatalf("text = %s", textOut.String())
	}
}

func TestSearchSuppressesOverlappingChunksFromSamePage(t *testing.T) {
	store := &dedupeStore{}
	service := Service{Embedder: embedderStub{}, Reranker: orderedReranker{}, Store: store}
	results, err := service.Search(context.Background(), "runtime config", "docs", "v1", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 || results[0].URL == results[1].URL {
		t.Fatalf("results = %+v", results)
	}
}

type orderedReranker struct{}

func (orderedReranker) Rerank(_ context.Context, _ string, documents []string) ([]rerank.Rank, error) {
	ranks := make([]rerank.Rank, len(documents))
	for i := range documents {
		ranks[i] = rerank.Rank{Index: i, Score: float64(len(documents) - i)}
	}
	return ranks, nil
}

type dedupeStore struct{}

func (*dedupeStore) ReplaceSnapshot(context.Context, vectorstore.Snapshot) error { return nil }
func (*dedupeStore) Search(context.Context, vectorstore.SearchRequest) ([]vectorstore.Candidate, error) {
	return []vectorstore.Candidate{
		{Point: vectorstore.Point{ID: "1", URL: "https://x/a", Markdown: "runtime config public values and private values"}, FusionScore: .03},
		{Point: vectorstore.Point{ID: "2", URL: "https://x/a", Markdown: "runtime config public values and private values example"}, FusionScore: .02},
		{Point: vectorstore.Point{ID: "3", URL: "https://x/b", Markdown: "define runtime configuration in nuxt config"}, FusionScore: .01},
	}, nil
}

func TestEmptyFormats(t *testing.T) {
	var output bytes.Buffer
	if err := WriteJSON(&output, []Result{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"results": []`) {
		t.Fatalf("json = %s", output.String())
	}
	output.Reset()
	if err := WriteText(&output, nil); err != nil {
		t.Fatal(err)
	}
	if output.String() != "No matching documentation found.\n" {
		t.Fatalf("text = %q", output.String())
	}
}
