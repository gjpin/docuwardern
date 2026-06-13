# Run scrape and indexing as one ingest workflow

Labels: `triage:ready`, `type:afk`

## What to build

Add `docuwarden ingest` to compose the production scrape and atomic indexing workflows. It accepts the same source, seed URL, selectors, version, and artifact output options as scrape, retains the generated artifact, and publishes it only when crawl policy permits.

An incomplete crawl must not replace a healthy index unless the caller explicitly opts into incomplete indexing. Cancellation and failures must propagate with useful exit codes while preserving diagnostic artifacts.

## Acceptance criteria

- One command can recursively scrape a documentation source, retain its artifact, index it, and make it searchable.
- Incomplete crawls stop before publication by default and preserve `manifest.json`, `report.json`, and successful Markdown documents.
- An explicit incomplete override is passed consistently through to indexing and is recorded in publication metadata.
- Interrupting the workflow cancels outstanding HTTP/model/Qdrant operations and does not switch aliases.
- An integration test covers successful ingest, crawl failure, indexing failure, and preservation of the previous active index.

## Blocked by

[Issue 003](003-crawl-failures-and-retries.md) and [Issue 006](006-atomic-index-replacement.md)
