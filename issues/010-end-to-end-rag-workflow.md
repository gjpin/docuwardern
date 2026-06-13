# Verify the complete documentation RAG workflow

Labels: `triage:ready`, `type:afk`

## What to build

Add a repeatable end-to-end verification environment for the complete CLI workflow. Use a controlled multi-page documentation fixture, Qdrant, and deterministic fake embedding and reranking services to exercise ingest and agent-facing search without external credentials.

The verification must prove recursive crawling, artifact creation, version publication, atomic replacement, source scoping, reranking, and both output formats through the compiled CLI.

## Acceptance criteria

- A single documented test command starts or provisions all required local dependencies and exercises the compiled binary.
- The fixture includes nested pages, duplicate navigation, an out-of-scope link, headings, tables, and fenced code.
- Tests prove initial ingest, search relevance, second-version publication, default-version selection, explicit-version search, and atomic replacement.
- Tests validate both JSON and prompt-ready text output and ensure diagnostics do not contaminate stdout.
- The complete automated test suite is suitable for CI and requires no hosted model or Qdrant credentials.

## Blocked by

[Issue 007](007-versioned-documentation.md), [Issue 008](008-prompt-ready-search-output.md), and [Issue 009](009-ingest-workflow.md)
