package rerank

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
)

type Rank struct {
	Index int
	Score float64
}

type Reranker interface {
	Rerank(ctx context.Context, query string, documents []string) ([]Rank, error)
}

type Cohere struct {
	Endpoint string
	Model    string
	APIKey   string
	Client   *http.Client
}

type Voyage struct {
	Endpoint string
	Model    string
	APIKey   string
	Client   *http.Client
}

func (adapter Cohere) Rerank(ctx context.Context, query string, documents []string) ([]Rank, error) {
	return rerank(ctx, adapter.Endpoint, adapter.Model, adapter.APIKey, adapter.Client, query, documents, "top_n")
}

func (adapter Voyage) Rerank(ctx context.Context, query string, documents []string) ([]Rank, error) {
	return rerank(ctx, adapter.Endpoint, adapter.Model, adapter.APIKey, adapter.Client, query, documents, "top_k")
}

func rerank(ctx context.Context, endpoint, model, apiKey string, client *http.Client, query string, documents []string, limitField string) ([]Rank, error) {
	if len(documents) == 0 {
		return nil, nil
	}
	if endpoint == "" || model == "" {
		return nil, errors.New("reranker endpoint and model are required")
	}
	body, err := json.Marshal(map[string]any{"model": model, "query": query, "documents": documents, limitField: len(documents)})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(endpoint, "/")+"/v1/rerank", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("rerank request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("reranker endpoint returned %s", response.Status)
	}
	var decoded struct {
		Results []struct {
			Index          int     `json:"index"`
			RelevanceScore float64 `json:"relevance_score"`
		} `json:"results"`
	}
	if err := json.NewDecoder(response.Body).Decode(&decoded); err != nil {
		return nil, fmt.Errorf("decode reranker response: %w", err)
	}
	if len(decoded.Results) > len(documents) {
		return nil, errors.New("reranker returned too many results")
	}
	seen := map[int]bool{}
	ranks := make([]Rank, len(decoded.Results))
	for i, item := range decoded.Results {
		if item.Index < 0 || item.Index >= len(documents) || seen[item.Index] {
			return nil, fmt.Errorf("invalid reranker index %d", item.Index)
		}
		seen[item.Index] = true
		ranks[i] = Rank{Index: item.Index, Score: item.RelevanceScore}
	}
	sort.SliceStable(ranks, func(i, j int) bool {
		if ranks[i].Score == ranks[j].Score {
			return ranks[i].Index < ranks[j].Index
		}
		return ranks[i].Score > ranks[j].Score
	})
	return ranks, nil
}
