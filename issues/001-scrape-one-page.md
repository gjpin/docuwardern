# Scrape one documentation page into a corpus artifact

Labels: `triage:ready`, `type:afk`

## What to build

Add the first usable `docuwarden scrape` path. Given a seed URL, explicit source ID, content CSS selector, optional documentation version, and output directory, fetch the page, extract the selected content, convert it to Markdown with `github.com/JohannesKaufmann/html-to-markdown/v2`, and write a reusable corpus artifact.

The artifact must contain the Markdown document, a schema-versioned manifest describing the source and document, and a crawl report. Decode HTTP responses to UTF-8 before conversion and resolve relative links against the page URL.

## Acceptance criteria

- `docuwarden scrape <url> --source <id> --content-selector <css> --output <dir>` produces `manifest.json`, `report.json`, and one Markdown document.
- `--version` is optional metadata and does not alter the requested URL.
- Missing required arguments, invalid selectors, non-HTML responses, fetch failures, and a missing content match return a nonzero exit code with an actionable error.
- Artifact filenames and document identifiers are deterministic and collision-safe.
- Tests use an HTTP fixture and golden Markdown output covering headings, links, tables, and fenced code.

## Blocked by

None - can start immediately.
