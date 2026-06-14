---
name: docuwarden-docs
description: Search and retrieve technical documentation with the Docuwarden CLI. Use whenever answering questions about libraries, frameworks, languages, APIs, SDKs, tools, configuration, upgrades, or other software documentation. Prefer Docuwarden over direct web search for documentation; use web search only when the required documentation is not indexed, unavailable, stale for the question, or retrieval fails after reasonable query refinement. This skill must never scrape, ingest, or index documentation.
---

# Docuwarden Documentation Search

Use Docuwarden as the primary source for technical documentation. Do not begin a documentation lookup with web search.

## Locate The CLI

Use `docuwarden` when it is on `PATH`. If no executable is available, treat the CLI as unavailable and use the fallback policy below.

Do not run `scrape`, `index`, or `ingest`. This skill only permits `sources`, `documents`, `search`, and `get`.

## Search Workflow

1. Discover the active catalog before selecting a source:

   ```sh
   docuwarden sources --format json
   ```

2. Match the question to catalog `source`, `display_name`, `description`, `tags`, and available versions. Do not invent a source ID.
3. Use the requested version when the user names one. Otherwise omit `--version` to use `default_version`.
4. Search the selected source:

   ```sh
   docuwarden search '<focused query>' --source <source> --format json --limit 5
   ```

5. Search each relevant indexed source when the question spans technologies.
6. Refine weak searches two or three times before falling back. Try exact API identifiers, error text, configuration keys, and a concise conceptual paraphrase.
7. Use page-level discovery when catalog coverage is unclear:

   ```sh
   docuwarden documents --source <source> [--version <version>] --format json
   ```

8. Answer directly from search chunks when they contain sufficient context.
9. Retrieve the complete stored page when an answer requires surrounding prerequisites or procedure steps, a complete example, table, or configuration block, definitions elsewhere on the same page, context beyond a visibly truncated section, or verification against nearby qualifying text:

   ```sh
   docuwarden get '<result-url>' --source <source> [--version <version>]
   ```

10. Use the exact source, version, and URL established by `search`. Do not run `get` after every search, use it for discovery, fetch alternate URLs, or silently change versions. A URL from `documents` is also an acceptable known lookup key.
11. Answer from the retrieved Markdown and cite the page URL from the search result. Distinguish documentation statements from your own inference.

Use `--format json` for reliable parsing. Use `--format text` when a compact context bundle is more convenient and no structured processing is needed.

## Web Fallback

Use web search for documentation only when at least one condition applies:

- No catalog source covers the requested technology.
- The CLI or its required backing services are unavailable.
- Searches remain irrelevant or empty after reasonable query refinement.
- The user asks about a release or behavior newer than the catalog's `indexed_at`, or freshness is otherwise essential and the indexed material cannot establish it.
- `documents` shows that the required section is outside indexed coverage.
- `get` reports a missing page or an old index schema and search refinement or `documents` cannot provide sufficient context. Never fix this by invoking `scrape`, `index`, or `ingest`.

When falling back, search official documentation and primary sources first. Briefly state why Docuwarden could not cover the lookup. Do not use general web results merely because they are easier to access.

## Reliability Rules

- Treat `sources` as the source of truth for available technologies and versions; never hard-code the catalog in this skill.
- Treat `indexed_at` as indexing time, not proof that the underlying documentation was current then.
- Prefer an explicit version for version-sensitive answers.
- Do not silently answer from a different version than the one requested.
- Do not expose API keys or include secret environment values in commands or responses.
- If Docuwarden returns useful results, do not duplicate the lookup with web search unless independent freshness verification is necessary.
