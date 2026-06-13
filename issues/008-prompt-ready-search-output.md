# Produce prompt-ready text search results

Labels: `triage:ready`, `type:afk`

## What to build

Add `--format text` to search so coding agents and humans can receive a compact Markdown context bundle instead of JSON. Render each result with its source URL, version and heading provenance, followed by the retrieved Markdown, while preserving reranked order.

JSON remains available through `--format json`, with a stable machine-readable schema and no log output mixed into stdout.

## Acceptance criteria

- `--format text` emits concise Markdown sections in reranked order with source attribution and content.
- `--format json` retains the structured result schema from the search workflow.
- Logs and diagnostics go to stderr; stdout contains only the selected result format.
- Empty results produce a valid empty JSON result or a clear, non-error text response.
- Golden tests cover JSON, text, special characters, code fences, empty results, and deterministic ordering.

## Blocked by

[Issue 005](005-search-with-reranking.md)
