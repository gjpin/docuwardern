# Recursively discover and scrape documentation subpages

Labels: `triage:ready`, `type:afk`

## What to build

Extend `docuwarden scrape` to accept repeatable `--link-selector` flags and recursively discover documentation pages from every successfully fetched page. Convert each accepted page into the same corpus artifact established by the single-page workflow.

Only URLs with the seed URL's normalized origin and path prefix may be crawled. Canonicalize URLs, remove fragments, deduplicate equivalent pages, and prevent crawl loops. Redirect targets must satisfy the same boundary before their content or links are accepted.

## Acceptance criteria

- Repeating `--link-selector` unions matches from all supplied selectors.
- A seed such as `https://nuxt.com/docs/4.x` accepts descendants under `/docs/4.x` and rejects sibling versions, unrelated paths, other origins, and deceptive path-prefix matches.
- Relative and absolute links, fragments, query strings, redirects, duplicate links, and cyclic navigation are handled deterministically.
- The artifact contains one document record and Markdown file per unique accepted page.
- HTTP fixture tests demonstrate recursive discovery across multiple levels and prove that out-of-scope URLs are never scraped.

## Blocked by

[Issue 001](001-scrape-one-page.md)
