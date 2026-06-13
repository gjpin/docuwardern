# Docuwarden Go Architecture

## Summary

Go module with a single `docuwarden` binary, independent packages for scraping, corpus artifacts, indexing, storage, and retrieval, plus CLI orchestration.

Data flow:

`website -> scrape artifact -> Markdown chunks -> dense + sparse vectors -> Qdrant hybrid search -> local reranking -> deduplicated CLI results`

## Key Modules

- `scrape`: Concurrent static HTML crawler. Enforces same-origin and seed-path scope, evaluates repeatable link selectors on every page, deduplicates canonical URLs, throttles requests, retries transient failures, and extracts the configured content selector.
- `markdown`: Converts extracted UTF-8 HTML using [`html-to-markdown/v2`](https://pkg.go.dev/github.com/JohannesKaufmann/html-to-markdown/v2), resolving relative links against each page URL.
- `corpus`: Defines documents, manifests, crawl reports, and filesystem artifact serialization.
- `chunk`: Heading-aware Markdown splitting. Preserve heading hierarchy and fenced code blocks, with configurable approximate token limit and overlap.
- `embedding`: Provider-neutral `Embedder` interface plus OpenAI-compatible and Voyage `/v1/embeddings` HTTP adapters.
- `sparse`: Deterministic lexical encoder preserving identifiers and splitting compound terms. Qdrant applies corpus-level IDF weighting.
- `rerank`: Provider-neutral `Reranker` interface plus Cohere-compatible and Voyage `/v1/rerank` HTTP adapters.
- `vectorstore`: Storage interface and Qdrant implementation using the [official Go client](https://github.com/qdrant/go-client).
- `retrieval`: Embeds and sparsely encodes queries, fuses dense and sparse candidates with RRF, reranks them, suppresses overlapping chunks, and formats results.
- `app`: Use-case orchestration for scrape, index, ingest, and search.
- `cmd/docuwarden`: Cobra-based CLI, flags, environment variables, exit codes, logging, and signal cancellation.

Dependencies point inward: adapters depend on core interfaces; scraping never imports Qdrant or model-provider code.

## Interfaces And Data

Core interfaces:

```go
type Converter interface {
	Convert(ctx context.Context, pageURL string, html io.Reader) (string, error)
}

type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

type Reranker interface {
	Rerank(ctx context.Context, query string, documents []string) ([]Rank, error)
}

type VectorStore interface {
	ReplaceSnapshot(ctx context.Context, snapshot Snapshot) error
	Search(ctx context.Context, request SearchRequest) ([]Candidate, error)
}
```

`SourceSpec` contains explicit source ID, seed URL, repeatable link selectors, one content selector, and optional version.

Each artifact contains:

- `manifest.json`: schema version, source ID, version, seed URL, selectors, timestamps, content hashes, document metadata, and completion status.
- `documents/*.md`: cleaned Markdown with collision-safe deterministic filenames.
- `report.json`: fetched, skipped, failed, redirected, and selector-missing pages.

Qdrant points use named `dense` and `sparse` vectors. Dense embedding input includes title, heading path, URL, and Markdown content; payloads retain the original Markdown plus source metadata. Point IDs are deterministic from source/version/URL/content hash/chunk index.

## CLI Contract

```text
docuwarden scrape <url> --source <id>
  --link-selector <css>... --content-selector <css>
  [--version <version>] [--output <dir>]

docuwarden index <artifact-dir> [--allow-incomplete]

docuwarden ingest <url> --source <id>
  --link-selector <css>... --content-selector <css>
  [--version <version>] [--output <dir>]

docuwarden search <query> --source <id>
  [--version <version>] [--limit 5] [--format json|text]

docuwarden sources [--format json|text]

docuwarden documents --source <id>
  [--version <version>] [--format json|text]
```

Selectors are repeatable flags only. Every selected link must remain under the normalized seed origin and path, so `https://nuxt.com/docs/4.x` accepts `/docs/4.x/getting-started/styling` but rejects `/docs/3.x` and unrelated site paths.

Configuration uses flags and environment variables. Secrets are environment-only. Provider flags select OpenAI-compatible, Cohere-compatible, or Voyage adapters; Voyage can use `VOYAGE_API_KEY` as a fallback credential.

## Indexing And Retrieval

- Default crawl policy: four workers, per-host throttling, bounded retries with backoff, request timeout, custom user agent, and no `robots.txt` enforcement.
- A failed or selector-missing page marks the artifact incomplete, preserves the report, and returns nonzero. Indexing rejects incomplete artifacts unless explicitly overridden.
- Default chunking: approximately 800 tokens with 100-token overlap. Heading paths are copied into each chunk; fenced code remains intact even when that creates an oversized chunk.
- Derive Qdrant vector size from the first embedding batch, validate all vectors, and use cosine distance.
- Voyage document embeddings use `input_type=document`; query embeddings use `input_type=query`.
- Build each indexing run in a new physical collection. After successful upload and validation, atomically update Qdrant aliases using its [alias API](https://api.qdrant.tech/api-reference/aliases/update-aliases).
- Maintain a version-specific alias and a source-default alias. Searching without `--version` uses the most recently indexed successful version for that source.
- Discover active corpora from aliases and collection metadata. `sources` reports source/version coverage, while `documents` scrolls page metadata without loading vectors.
- Search retrieves 40 hybrid candidates by default, fuses dense and sparse rankings with Reciprocal Rank Fusion, reranks them locally, suppresses near-duplicate chunks, and returns the best five.
- JSON output includes rank, dense score, sparse score, fusion score, reranker score, source, version, URL, title, heading path, and Markdown content. Text output produces a compact Markdown context bundle.

## Test Plan

- URL boundary tests for sibling versions, path-prefix traps, fragments, redirects, relative links, duplicate links, and crawl loops.
- HTTP fixture tests for recursive discovery, throttling, retries, charset conversion, missing selectors, and partial crawl reports.
- Golden tests for extracted HTML-to-Markdown conversion, tables, links, headings, and code blocks.
- Artifact schema round-trip and deterministic document/chunk ID tests.
- Chunking tests for nested headings, oversized sections, overlap, and fenced code.
- Contract tests for OpenAI-compatible and Voyage embeddings, plus Cohere-compatible and Voyage reranking.
- Qdrant integration tests with a container: collection creation, upload, alias swap, rollback on failure, and stale snapshot cleanup.
- End-to-end test covering scrape, index, search, rerank, JSON output, and version selection.

## Assumptions

- Go 1.26 is the initial toolchain.
- Only public static HTML documentation is supported in v1; browser rendering and authenticated sites are excluded.
- Version is indexing metadata and does not alter crawl URLs.
- Markdown and metadata are retained; raw HTML is transient.
- The converter uses the latest compatible v2 release. Its input must be decoded to UTF-8 before conversion.
- Global cross-source search and MCP serving are deferred; the package interfaces allow both later.
