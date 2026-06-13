package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/zero/docuwarden/chunk"
	"github.com/zero/docuwarden/corpus"
	"github.com/zero/docuwarden/embedding"
	"github.com/zero/docuwarden/retrieval"
	"github.com/zero/docuwarden/scrape"
	"github.com/zero/docuwarden/sparse"
	"github.com/zero/docuwarden/vectorstore"
)

type Service struct {
	Embedder      embedding.Embedder
	SparseEncoder sparse.Encoder
	Store         vectorstore.VectorStore
	Search        retrieval.Service
	Progress      func(format string, args ...any)
}

type IndexOptions struct {
	AllowIncomplete bool
	BatchSize       int
	Chunk           chunk.Config
	Retention       int
	EmbeddingModel  string
}

type RetryOptions struct {
	ContentSelectors []string
	Workers          int
	WorkersSet       bool
	Throttle         time.Duration
	ThrottleSet      bool
	Timeout          time.Duration
	TimeoutSet       bool
	MaxRetries       int
	MaxRetriesSet    bool
	Backoff          time.Duration
	BackoffSet       bool
	HTTPClient       *http.Client
	Now              func() time.Time
	Sleep            func(context.Context, time.Duration) error
	Progress         func(string, ...any)
}

func Scrape(ctx context.Context, cfg scrape.Config, output string) (corpus.Artifact, error) {
	return scrapeArtifact(ctx, cfg, output, cfg.Progress)
}

func Retry(ctx context.Context, artifactDir string, options RetryOptions) (corpus.Artifact, error) {
	artifact, err := corpus.Read(artifactDir)
	if err != nil {
		return corpus.Artifact{}, err
	}
	if artifact.Manifest.Complete {
		return artifact, nil
	}

	source := artifact.Manifest.Source
	source.ContentSelectors = appendUniqueExcluding(source.ContentSelectors, source.ContentSelector, options.ContentSelectors...)
	settings := artifact.Manifest.Crawl
	if settings.Workers <= 0 {
		settings.Workers = 4
	}
	if settings.Timeout <= 0 {
		settings.Timeout = 20 * time.Second
	}
	if settings.Backoff <= 0 {
		settings.Backoff = 200 * time.Millisecond
	}
	if artifact.Manifest.Crawl == (corpus.CrawlSettings{}) {
		settings.Throttle = 100 * time.Millisecond
		settings.MaxRetries = 3
	}
	if options.WorkersSet {
		settings.Workers = options.Workers
	}
	if options.ThrottleSet {
		settings.Throttle = options.Throttle
	}
	if options.TimeoutSet {
		settings.Timeout = options.Timeout
	}
	if options.MaxRetriesSet {
		settings.MaxRetries = options.MaxRetries
	}
	if options.BackoffSet {
		settings.Backoff = options.Backoff
	}
	if settings.Workers <= 0 || settings.MaxRetries < 0 {
		return artifact, errors.New("workers must be positive and retries non-negative")
	}

	initial := incompleteURLs(artifact.Report)
	known := make([]string, len(artifact.Manifest.Documents))
	for i, document := range artifact.Manifest.Documents {
		known[i] = document.URL
	}
	cfg := scrape.Config{Source: source, Workers: settings.Workers, Throttle: settings.Throttle, Timeout: settings.Timeout, MaxRetries: settings.MaxRetries, Backoff: settings.Backoff, HTTPClient: options.HTTPClient, Now: options.Now, Sleep: options.Sleep, Progress: options.Progress}
	retried, crawlErr := scrape.CrawlTargets(ctx, cfg, initial, known)
	if retried.Manifest.SchemaVersion == 0 {
		return artifact, crawlErr
	}
	merged := mergeRetry(artifact, retried, initial)
	merged.Manifest.Source = source
	merged.Manifest.Crawl = retried.Manifest.Crawl
	merged.Manifest.CompletedAt = retried.Manifest.CompletedAt
	merged.Manifest.Complete = len(merged.Report.Failed) == 0 && len(merged.Report.SelectorMissing) == 0
	corpus.Sort(&merged)
	if err := corpus.Write(artifactDir, merged); err != nil {
		return merged, errors.Join(crawlErr, err)
	}
	if !merged.Manifest.Complete {
		return merged, &scrape.CrawlError{Artifact: merged}
	}
	return merged, nil
}

func appendUnique(existing []string, additions ...string) []string {
	seen := make(map[string]bool, len(existing)+len(additions))
	result := make([]string, 0, len(existing)+len(additions))
	for _, value := range append(append([]string(nil), existing...), additions...) {
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}

func appendUniqueExcluding(existing []string, excluded string, additions ...string) []string {
	seen := map[string]bool{excluded: true}
	result := make([]string, 0, len(existing)+len(additions))
	for _, value := range append(append([]string(nil), existing...), additions...) {
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}

func incompleteURLs(report corpus.Report) []string {
	seen := map[string]bool{}
	var urls []string
	for _, event := range append(append([]corpus.PageEvent(nil), report.Failed...), report.SelectorMissing...) {
		if !seen[event.URL] {
			seen[event.URL] = true
			urls = append(urls, event.URL)
		}
	}
	return urls
}

func mergeRetry(existing, retried corpus.Artifact, initial []string) corpus.Artifact {
	targeted := map[string]bool{}
	for _, pageURL := range initial {
		targeted[pageURL] = true
	}
	existing.Report.Failed = removeEvents(existing.Report.Failed, targeted)
	existing.Report.SelectorMissing = removeEvents(existing.Report.SelectorMissing, targeted)
	existing.Report.Fetched = mergeEvents(existing.Report.Fetched, retried.Report.Fetched)
	existing.Report.Redirected = mergeEvents(existing.Report.Redirected, retried.Report.Redirected)
	existing.Report.Skipped = mergeEvents(existing.Report.Skipped, retried.Report.Skipped)
	existing.Report.Failed = mergeEvents(existing.Report.Failed, retried.Report.Failed)
	existing.Report.SelectorMissing = mergeEvents(existing.Report.SelectorMissing, retried.Report.SelectorMissing)

	byURL := make(map[string]int, len(existing.Manifest.Documents))
	for i, document := range existing.Manifest.Documents {
		byURL[document.URL] = i
	}
	for _, document := range retried.Manifest.Documents {
		if index, ok := byURL[document.URL]; ok {
			delete(existing.Markdown, existing.Manifest.Documents[index].ID)
			existing.Manifest.Documents[index] = document
		} else {
			byURL[document.URL] = len(existing.Manifest.Documents)
			existing.Manifest.Documents = append(existing.Manifest.Documents, document)
		}
		existing.Markdown[document.ID] = retried.Markdown[document.ID]
	}
	return existing
}

func removeEvents(events []corpus.PageEvent, removed map[string]bool) []corpus.PageEvent {
	result := events[:0]
	for _, event := range events {
		if !removed[event.URL] {
			result = append(result, event)
		}
	}
	return result
}

func mergeEvents(existing, additions []corpus.PageEvent) []corpus.PageEvent {
	seen := map[string]bool{}
	result := make([]corpus.PageEvent, 0, len(existing)+len(additions))
	for _, event := range append(append([]corpus.PageEvent(nil), existing...), additions...) {
		key := fmt.Sprintf("%s\x00%d\x00%s\x00%s", event.URL, event.StatusCode, event.Detail, event.Target)
		if !seen[key] {
			seen[key] = true
			result = append(result, event)
		}
	}
	return result
}

func scrapeArtifact(ctx context.Context, cfg scrape.Config, output string, progress func(string, ...any)) (corpus.Artifact, error) {
	if progress != nil {
		progress("crawl: starting %s", cfg.Source.SeedURL)
		cfg.Progress = progress
	}
	artifact, crawlErr := scrape.Crawl(ctx, cfg)
	if artifact.Manifest.SchemaVersion != 0 {
		if progress != nil {
			progress("artifact: writing %d document(s) to %s", len(artifact.Manifest.Documents), output)
		}
		if err := corpus.Write(output, artifact); err != nil {
			return artifact, errors.Join(crawlErr, err)
		}
		if progress != nil {
			progress("artifact: written to %s", output)
		}
	}
	return artifact, crawlErr
}

func (service Service) Index(ctx context.Context, artifactDir string, options IndexOptions) error {
	service.progress("index: reading artifact from %s", artifactDir)
	artifact, err := corpus.Read(artifactDir)
	if err != nil {
		return err
	}
	if !artifact.Manifest.Complete && !options.AllowIncomplete {
		return errors.New("artifact is incomplete; pass --allow-incomplete to override")
	}
	if options.BatchSize <= 0 {
		options.BatchSize = 64
	}
	sparseEncoder := service.SparseEncoder
	if sparseEncoder == nil {
		sparseEncoder = sparse.LexicalEncoder{}
	}
	var points []vectorstore.Point
	service.progress("index: chunking %d document(s)", len(artifact.Manifest.Documents))
	for _, document := range artifact.Manifest.Documents {
		chunks, err := chunk.Split(artifact.Markdown[document.ID], options.Chunk)
		if err != nil {
			return fmt.Errorf("chunk %s: %w", document.URL, err)
		}
		for _, item := range chunks {
			point := vectorstore.Point{ID: pointID(artifact.Manifest.Source.SourceID, artifact.Manifest.Source.Version, document.URL, document.ContentHash, item.Index), Source: artifact.Manifest.Source.SourceID, Version: artifact.Manifest.Source.Version, URL: document.URL, Title: document.Title, HeadingPath: item.HeadingPath, ChunkIndex: item.Index, Markdown: item.Markdown, ContentHash: document.ContentHash, CrawledAt: document.CrawledAt}
			encoded := sparseEncoder.Encode(indexText(point))
			point.Sparse = vectorstore.SparseVector{Indices: encoded.Indices, Values: encoded.Values}
			points = append(points, point)
		}
	}
	if len(points) == 0 {
		return errors.New("artifact contains no indexable chunks")
	}
	service.progress("index: prepared %d chunk(s)", len(points))
	dimension := 0
	for start := 0; start < len(points); start += options.BatchSize {
		end := min(start+options.BatchSize, len(points))
		service.progress("index: embedding chunks %d-%d of %d", start+1, end, len(points))
		texts := make([]string, end-start)
		for i := start; i < end; i++ {
			texts[i-start] = indexText(points[i])
		}
		vectors, err := service.Embedder.Embed(ctx, texts)
		if err != nil {
			return fmt.Errorf("embed chunks: %w", err)
		}
		if len(vectors) != len(texts) {
			return fmt.Errorf("embedding count mismatch: got %d, want %d", len(vectors), len(texts))
		}
		for i, vector := range vectors {
			if dimension == 0 {
				dimension = len(vector)
			}
			if dimension == 0 || len(vector) != dimension {
				return fmt.Errorf("embedding dimension mismatch at chunk %d", start+i)
			}
			points[start+i].DenseVector = vector
		}
	}
	service.progress("index: publishing %d chunk(s) to vector store", len(points))
	err = service.Store.ReplaceSnapshot(ctx, vectorstore.Snapshot{
		Source: artifact.Manifest.Source.SourceID, Version: artifact.Manifest.Source.Version,
		DisplayName: artifact.Manifest.Source.DisplayName, Description: artifact.Manifest.Source.Description,
		Tags: artifact.Manifest.Source.Tags, SeedURL: artifact.Manifest.Source.SeedURL,
		DocumentCount: len(artifact.Manifest.Documents), Complete: artifact.Manifest.Complete,
		IndexedAt: time.Now().UTC(), EmbeddingModel: options.EmbeddingModel,
		Points: points, AllowIncomplete: options.AllowIncomplete, Retention: options.Retention,
	})
	if err != nil {
		return err
	}
	service.progress("index: publication complete")
	return nil
}

func indexText(point vectorstore.Point) string {
	var builder strings.Builder
	if point.Title != "" {
		fmt.Fprintf(&builder, "Title: %s\n", point.Title)
	}
	if len(point.HeadingPath) > 0 {
		fmt.Fprintf(&builder, "Headings: %s\n", strings.Join(point.HeadingPath, " > "))
	}
	if point.URL != "" {
		fmt.Fprintf(&builder, "URL: %s\n", point.URL)
	}
	builder.WriteString("Content:\n")
	builder.WriteString(point.Markdown)
	return builder.String()
}

func (service Service) Ingest(ctx context.Context, cfg scrape.Config, output string, options IndexOptions) error {
	service.progress("ingest: crawl phase")
	artifact, err := scrapeArtifact(ctx, cfg, output, service.Progress)
	if err != nil && !(options.AllowIncomplete && artifact.Manifest.SchemaVersion != 0) {
		return err
	}
	if indexErr := service.Index(ctx, output, options); indexErr != nil {
		return errors.Join(err, indexErr)
	}
	service.progress("ingest: complete")
	if options.AllowIncomplete {
		return nil
	}
	return err
}

func (service Service) progress(format string, args ...any) {
	if service.Progress != nil {
		service.Progress(format, args...)
	}
}

func pointID(source, version, url, contentHash string, index int) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%d", source, version, url, contentHash, index)))
	return hex.EncodeToString(sum[:])
}
