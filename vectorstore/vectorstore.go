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
	CrawledAt   time.Time
}

type SparseVector struct {
	Indices []uint32
	Values  []float32
}

type Snapshot struct {
	Source          string
	Version         string
	Points          []Point
	AllowIncomplete bool
	Retention       int
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
	ReplaceSnapshot(ctx context.Context, snapshot Snapshot) error
	Search(ctx context.Context, request SearchRequest) ([]Candidate, error)
}
