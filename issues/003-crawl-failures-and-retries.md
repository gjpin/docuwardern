# Report incomplete crawls and retry transient failures

Labels: `triage:ready`, `type:afk`

## What to build

Make recursive scraping resilient and diagnosable. Crawl with bounded concurrency, per-host throttling, request timeouts, a recognizable user agent, and bounded exponential backoff for transient failures. Preserve successful documents and a complete report when any page fails.

Any exhausted request failure or page without the content selector marks the artifact incomplete and makes the command exit nonzero. The artifact remains available for diagnosis or explicitly overridden indexing.

## Acceptance criteria

- The default crawl uses four workers while enforcing configurable per-host throttling and request timeouts.
- Transient network errors, HTTP 429, and HTTP 5xx responses are retried with bounded backoff; permanent HTTP errors are not repeatedly retried.
- `report.json` distinguishes fetched, redirected, skipped, failed, and selector-missing pages and records useful failure details.
- `manifest.json` records whether the artifact is complete.
- A partially failed crawl preserves successful Markdown files, returns nonzero, and has deterministic tests for retry exhaustion and missing selectors.

## Blocked by

[Issue 002](002-recursive-documentation-crawl.md)
