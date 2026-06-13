package retrieval

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"unicode"

	"github.com/zero/docuwarden/embedding"
	"github.com/zero/docuwarden/rerank"
	"github.com/zero/docuwarden/sparse"
	"github.com/zero/docuwarden/vectorstore"
)

type Result struct {
	Rank          int      `json:"rank"`
	VectorScore   float64  `json:"vector_score"`
	DenseScore    float64  `json:"dense_score"`
	SparseScore   float64  `json:"sparse_score"`
	FusionScore   float64  `json:"fusion_score"`
	RerankerScore float64  `json:"reranker_score"`
	Source        string   `json:"source"`
	Version       string   `json:"version,omitempty"`
	URL           string   `json:"url"`
	Title         string   `json:"title,omitempty"`
	HeadingPath   []string `json:"heading_path,omitempty"`
	Markdown      string   `json:"markdown"`
}

type Service struct {
	Embedder      embedding.Embedder
	Reranker      rerank.Reranker
	Store         vectorstore.VectorStore
	SparseEncoder sparse.Encoder
	Candidates    int
	Mode          vectorstore.SearchMode
}

func (service Service) Search(ctx context.Context, query, source, version string, limit int) ([]Result, error) {
	if query == "" || source == "" {
		return nil, fmt.Errorf("query and source are required")
	}
	if limit <= 0 {
		limit = 5
	}
	candidateLimit := service.Candidates
	if candidateLimit <= 0 {
		candidateLimit = 40
	}
	if candidateLimit < limit {
		candidateLimit = limit
	}
	vectors, err := service.Embedder.Embed(ctx, []string{query})
	if err != nil {
		return nil, err
	}
	if len(vectors) != 1 {
		return nil, fmt.Errorf("query embedding returned %d vectors", len(vectors))
	}
	mode := service.Mode
	if mode == "" {
		mode = vectorstore.SearchModeHybrid
	}
	encoder := service.SparseEncoder
	if encoder == nil {
		encoder = sparse.LexicalEncoder{}
	}
	sparseQuery := encoder.Encode(query)
	candidates, err := service.Store.Search(ctx, vectorstore.SearchRequest{Source: source, Version: version, Dense: vectors[0], Sparse: vectorstore.SparseVector{Indices: sparseQuery.Indices, Values: sparseQuery.Values}, Limit: candidateLimit, Mode: mode})
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return []Result{}, nil
	}
	documents := make([]string, len(candidates))
	for i := range candidates {
		documents[i] = candidates[i].Markdown
	}
	ranks, err := service.Reranker.Rerank(ctx, query, documents)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(ranks, func(i, j int) bool {
		if ranks[i].Score == ranks[j].Score {
			return tieScore(candidates[ranks[i].Index]) > tieScore(candidates[ranks[j].Index])
		}
		return ranks[i].Score > ranks[j].Score
	})
	results := make([]Result, 0, limit)
	var selected []vectorstore.Candidate
	for _, rank := range ranks {
		candidate := candidates[rank.Index]
		if duplicateCandidate(candidate, selected) {
			continue
		}
		selected = append(selected, candidate)
		vectorScore := candidate.FusionScore
		if mode == vectorstore.SearchModeDense {
			vectorScore = candidate.DenseScore
		}
		results = append(results, Result{Rank: len(results) + 1, VectorScore: vectorScore, DenseScore: candidate.DenseScore, SparseScore: candidate.SparseScore, FusionScore: candidate.FusionScore, RerankerScore: rank.Score, Source: candidate.Source, Version: candidate.Version, URL: candidate.URL, Title: candidate.Title, HeadingPath: candidate.HeadingPath, Markdown: candidate.Markdown})
		if len(results) == limit {
			break
		}
	}
	return results, nil
}

func tieScore(candidate vectorstore.Candidate) float64 {
	if candidate.FusionScore != 0 {
		return candidate.FusionScore
	}
	return candidate.DenseScore
}

func duplicateCandidate(candidate vectorstore.Candidate, selected []vectorstore.Candidate) bool {
	for _, existing := range selected {
		if candidate.ID != "" && candidate.ID == existing.ID {
			return true
		}
		if normalize(candidate.Markdown) == normalize(existing.Markdown) {
			return true
		}
		if candidate.URL == existing.URL && tokenJaccard(candidate.Markdown, existing.Markdown) >= 0.75 {
			return true
		}
	}
	return false
}

func normalize(text string) string { return strings.Join(strings.Fields(strings.ToLower(text)), " ") }

func tokenJaccard(left, right string) float64 {
	a := wordSet(left)
	b := wordSet(right)
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	intersection := 0
	for token := range a {
		if b[token] {
			intersection++
		}
	}
	union := len(a) + len(b) - intersection
	return float64(intersection) / float64(union)
}

func wordSet(text string) map[string]bool {
	result := map[string]bool{}
	fields := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' })
	for _, field := range fields {
		if len(field) > 1 {
			result[field] = true
		}
	}
	return result
}

func WriteJSON(out io.Writer, results []Result) error {
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(struct {
		Results []Result `json:"results"`
	}{Results: results})
}

func WriteText(out io.Writer, results []Result) error {
	if len(results) == 0 {
		_, err := io.WriteString(out, "No matching documentation found.\n")
		return err
	}
	for _, result := range results {
		heading := strings.Join(result.HeadingPath, " > ")
		if _, err := fmt.Fprintf(out, "## %d. %s\n\nSource: %s", result.Rank, fallback(result.Title, result.URL), result.URL); err != nil {
			return err
		}
		if result.Version != "" {
			if _, err := fmt.Fprintf(out, "\nVersion: %s", result.Version); err != nil {
				return err
			}
		}
		if heading != "" {
			if _, err := fmt.Fprintf(out, "\nHeading: %s", heading); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(out, "\n\n%s\n", strings.TrimSpace(result.Markdown)); err != nil {
			return err
		}
		if result.Rank < len(results) {
			if _, err := io.WriteString(out, "\n---\n\n"); err != nil {
				return err
			}
		}
	}
	return nil
}

func fallback(value, alternative string) string {
	if value != "" {
		return value
	}
	return alternative
}
