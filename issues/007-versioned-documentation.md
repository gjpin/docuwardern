# Support versioned documentation indexes

Labels: `triage:ready`, `type:afk`

## What to build

Extend indexing and search so one source can retain independently addressable documentation versions. Publish a stable alias for each source/version pair and a source-default alias pointing to the most recently indexed successful version.

`docuwarden search --version <version>` must query that exact version. Omitting the flag must resolve through the source-default alias without requiring agents to know the current version.

## Acceptance criteria

- Indexing versioned artifacts creates or replaces only that source/version index.
- Successful version publication updates the source-default alias to that version; a failed publication changes neither alias.
- Explicit-version search never returns chunks from another version.
- Search without `--version` returns the most recently indexed successful version for the source.
- Integration tests index two versions, search each explicitly, verify default selection, and verify replacement isolation.

## Blocked by

[Issue 005](005-search-with-reranking.md) and [Issue 006](006-atomic-index-replacement.md)
