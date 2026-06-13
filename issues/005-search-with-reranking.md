# Search indexed documentation with reranking

Labels: `triage:ready`, `type:afk`

## What to build

Add `docuwarden search <query> --source <id>` for coding-agent retrieval. Embed the query through the configured OpenAI-compatible endpoint, retrieve vector candidates from the source's Qdrant index, rerank their Markdown through a configurable Cohere-compatible `/v1/rerank` endpoint, and return the best results as JSON.

The command must expose provider and storage failures clearly and preserve both vector and reranker scores for diagnostics.

## Acceptance criteria

- Search requires a source ID, retrieves 20 candidates by default, and returns five reranked results by default.
- The reranker is accessed through a provider-neutral interface with a Cohere-compatible HTTP adapter.
- JSON output includes rank, vector score, reranker score, source, version, URL, title, heading path, and Markdown content.
- `--limit` controls final result count without reducing candidate retrieval below the amount needed for reranking.
- Contract tests cover embeddings and reranking requests, malformed responses, timeouts, and deterministic result ordering; an integration test searches an indexed fixture corpus.

## Blocked by

[Issue 004](004-index-corpus-in-qdrant.md)
