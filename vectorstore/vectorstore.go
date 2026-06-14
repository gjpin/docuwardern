package vectorstore

import (
	"context"
	"time"
)

type Point struct {
	ID          string
	DenseVector []float32
	Sparse      SparseVector
	Source      string
	Version     string
	URL         string
	Title       string
	HeadingPath []string
	ChunkIndex  int
	Markdown    string
	ContentHash string
	InputHash   string
	CrawledAt   time.Time
}

type SparseVector struct {
	Indices []uint32
	Values  []float32
}

type Snapshot struct {
	Source           string
	Version          string
	DisplayName      string
	Description      string
	Tags             []string
	SeedURL          string
	DocumentCount    int
	Complete         bool
	IndexedAt        time.Time
	EmbeddingModel   string
	EmbeddingProfile string
	Points           []Point
	AllowIncomplete  bool
	Retention        int
}

type Catalog struct {
	SchemaVersion int             `json:"schema_version"`
	Sources       []CatalogSource `json:"sources"`
}

type CatalogSource struct {
	Source         string           `json:"source"`
	DisplayName    string           `json:"display_name,omitempty"`
	Description    string           `json:"description,omitempty"`
	Tags           []string         `json:"tags,omitempty"`
	DefaultVersion string           `json:"default_version,omitempty"`
	Versions       []CatalogVersion `json:"versions"`
}

type CatalogVersion struct {
	Version        string `json:"version,omitempty"`
	SeedURL        string `json:"seed_url,omitempty"`
	DocumentCount  int    `json:"document_count"`
	ChunkCount     int    `json:"chunk_count"`
	IndexedAt      string `json:"indexed_at,omitempty"`
	Complete       bool   `json:"complete"`
	EmbeddingModel string `json:"embedding_model,omitempty"`
}

type DocumentCatalog struct {
	SchemaVersion int               `json:"schema_version"`
	Source        string            `json:"source"`
	Version       string            `json:"version,omitempty"`
	Documents     []CatalogDocument `json:"documents"`
}

type CatalogDocument struct {
	URL       string `json:"url"`
	Title     string `json:"title,omitempty"`
	CrawledAt string `json:"crawled_at,omitempty"`
}

type CatalogStore interface {
	ListSources(ctx context.Context) (Catalog, error)
	ListDocuments(ctx context.Context, source, version string) (DocumentCatalog, error)
}

type Candidate struct {
	Point
	DenseScore  float64
	SparseScore float64
	FusionScore float64
}

type SearchMode string

const (
	SearchModeHybrid SearchMode = "hybrid"
	SearchModeDense  SearchMode = "dense"
)

type SearchRequest struct {
	Source  string
	Version string
	Dense   []float32
	Sparse  SparseVector
	Limit   int
	Mode    SearchMode
}

type VectorStore interface {
	LoadCachedDenseVectors(ctx context.Context, source, version, embeddingProfile string, inputHashes []string) (map[string][]float32, error)
	ReplaceSnapshot(ctx context.Context, snapshot Snapshot) error
	Search(ctx context.Context, request SearchRequest) ([]Candidate, error)
}
