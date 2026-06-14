package embedding

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strings"
)

const InputFormatVersion = 1

type Profile struct {
	Provider           string
	Endpoint           string
	Model              string
	InputType          string
	InputFormatVersion int
}

func (profile Profile) Fingerprint() string {
	version := profile.InputFormatVersion
	if version == 0 {
		version = InputFormatVersion
	}
	value := fmt.Sprintf("provider=%s\nendpoint=%s\nmodel=%s\ninput_type=%s\ninput_format=%d",
		strings.ToLower(strings.TrimSpace(profile.Provider)), normalizeEndpoint(profile.Endpoint),
		strings.TrimSpace(profile.Model), strings.TrimSpace(profile.InputType), version)
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func normalizeEndpoint(endpoint string) string {
	value := strings.TrimSpace(endpoint)
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return strings.TrimRight(value, "/")
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	parsed.RawQuery = ""
	parsed.Fragment = ""
	parsed.Path = strings.TrimSuffix(path.Clean("/"+strings.TrimPrefix(parsed.Path, "/")), "/")
	if parsed.Path == "." || parsed.Path == "/" {
		parsed.Path = ""
	}
	return parsed.String()
}

type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

type OpenAI struct {
	Endpoint string
	Model    string
	APIKey   string
	Client   *http.Client
}

type Voyage struct {
	Endpoint  string
	Model     string
	APIKey    string
	InputType string
	Client    *http.Client
}

func (adapter OpenAI) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	if adapter.Endpoint == "" || adapter.Model == "" {
		return nil, errors.New("embedding endpoint and model are required")
	}
	body, err := json.Marshal(map[string]any{"model": adapter.Model, "input": texts, "encoding_format": "float"})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(adapter.Endpoint, "/")+"/v1/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if adapter.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+adapter.APIKey)
	}
	client := adapter.Client
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embedding request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("embedding endpoint returned %s", response.Status)
	}
	return decodeResponse(response, texts)
}

func (adapter Voyage) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	if adapter.Endpoint == "" || adapter.Model == "" {
		return nil, errors.New("embedding endpoint and model are required")
	}
	body, err := json.Marshal(map[string]any{"model": adapter.Model, "input": texts, "input_type": adapter.InputType})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(adapter.Endpoint, "/")+"/v1/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if adapter.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+adapter.APIKey)
	}
	client := adapter.Client
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embedding request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("embedding endpoint returned %s", response.Status)
	}
	return decodeResponse(response, texts)
}

func decodeResponse(response *http.Response, texts []string) ([][]float32, error) {
	var decoded struct {
		Data []struct {
			Index     int       `json:"index"`
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&decoded); err != nil {
		return nil, fmt.Errorf("decode embedding response: %w", err)
	}
	if len(decoded.Data) != len(texts) {
		return nil, fmt.Errorf("embedding response count %d does not match input count %d", len(decoded.Data), len(texts))
	}
	sort.Slice(decoded.Data, func(i, j int) bool { return decoded.Data[i].Index < decoded.Data[j].Index })
	vectors := make([][]float32, len(decoded.Data))
	dimension := 0
	for i, item := range decoded.Data {
		if item.Index != i || len(item.Embedding) == 0 {
			return nil, fmt.Errorf("invalid embedding at index %d", i)
		}
		if dimension == 0 {
			dimension = len(item.Embedding)
		}
		if len(item.Embedding) != dimension {
			return nil, fmt.Errorf("embedding dimension mismatch at index %d", i)
		}
		vectors[i] = item.Embedding
	}
	return vectors, nil
}
