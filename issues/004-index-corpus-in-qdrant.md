# Index a corpus artifact into Qdrant

Labels: `triage:ready`, `type:afk`

## What to build

Add `docuwarden index <artifact-dir>` as a complete indexing path. Validate the corpus artifact, split its Markdown into heading-aware chunks, obtain vectors from a configurable OpenAI-compatible embeddings endpoint, and upload searchable points through the official Qdrant Go client.

Chunk metadata must retain enough source context for retrieval: source, version, URL, title, heading path, chunk index, Markdown content, content hash, and crawl timestamp. Incomplete artifacts are rejected unless `--allow-incomplete` is supplied.

## Acceptance criteria

- Heading-aware chunking targets approximately 800 tokens with 100-token overlap and preserves heading hierarchy and fenced code blocks.
- Embeddings are requested in batches through a provider-neutral interface and OpenAI-compatible HTTP adapter configured by flags and namespaced environment variables.
- Vector size is derived from the first embedding batch, all later vectors are validated, and the Qdrant collection uses cosine distance.
- Point identifiers are deterministic from source, version, URL, content hash, and chunk index.
- Contract tests cover the embeddings adapter and chunking edge cases; a Qdrant integration test proves uploaded corpus chunks can be retrieved by vector similarity.

## Blocked by

[Issue 001](001-scrape-one-page.md)
