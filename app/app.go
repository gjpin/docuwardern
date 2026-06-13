package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
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
}

type IndexOptions struct {
	AllowIncomplete bool
	BatchSize       int
	Chunk           chunk.Config
	Retention       int
	EmbeddingModel  string
}

func Scrape(ctx context.Context, cfg scrape.Config, output string) (corpus.Artifact, error) {
	artifact, crawlErr := scrape.Crawl(ctx, cfg)
	if artifact.Manifest.SchemaVersion != 0 {
		if err := corpus.Write(output, artifact); err != nil {
			return artifact, errors.Join(crawlErr, err)
		}
	}
	return artifact, crawlErr
}

func (service Service) Index(ctx context.Context, artifactDir string, options IndexOptions) error {
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
	dimension := 0
	for start := 0; start < len(points); start += options.BatchSize {
		end := min(start+options.BatchSize, len(points))
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
	return service.Store.ReplaceSnapshot(ctx, vectorstore.Snapshot{
		Source: artifact.Manifest.Source.SourceID, Version: artifact.Manifest.Source.Version,
		DisplayName: artifact.Manifest.Source.DisplayName, Description: artifact.Manifest.Source.Description,
		Tags: artifact.Manifest.Source.Tags, SeedURL: artifact.Manifest.Source.SeedURL,
		DocumentCount: len(artifact.Manifest.Documents), Complete: artifact.Manifest.Complete,
		IndexedAt: time.Now().UTC(), EmbeddingModel: options.EmbeddingModel,
		Points: points, AllowIncomplete: options.AllowIncomplete, Retention: options.Retention,
	})
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
	artifact, err := Scrape(ctx, cfg, output)
	if err != nil && !(options.AllowIncomplete && artifact.Manifest.SchemaVersion != 0) {
		return err
	}
	if indexErr := service.Index(ctx, output, options); indexErr != nil {
		return errors.Join(err, indexErr)
	}
	if options.AllowIncomplete {
		return nil
	}
	return err
}

func pointID(source, version, url, contentHash string, index int) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%d", source, version, url, contentHash, index)))
	return hex.EncodeToString(sum[:])
}
