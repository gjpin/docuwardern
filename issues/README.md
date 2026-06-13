# Docuwarden Issues

These issues are ordered by dependency. Each is a thin, independently verifiable vertical slice.

## Triage Labels

- `triage:ready`: sufficiently specified for implementation.
- `type:afk`: can be implemented and merged without human interaction.
- `type:hitl`: requires a human decision or review before completion.

## Issue Index

1. [Scrape one documentation page into a corpus artifact](001-scrape-one-page.md)
2. [Recursively discover and scrape documentation subpages](002-recursive-documentation-crawl.md)
3. [Report incomplete crawls and retry transient failures](003-crawl-failures-and-retries.md)
4. [Index a corpus artifact into Qdrant](004-index-corpus-in-qdrant.md)
5. [Search indexed documentation with reranking](005-search-with-reranking.md)
6. [Atomically replace an existing documentation index](006-atomic-index-replacement.md)
7. [Support versioned documentation indexes](007-versioned-documentation.md)
8. [Produce prompt-ready text search results](008-prompt-ready-search-output.md)
9. [Run scrape and indexing as one ingest workflow](009-ingest-workflow.md)
10. [Verify the complete documentation RAG workflow](010-end-to-end-rag-workflow.md)
