# Docuwarden CLI Reference

Docuwarden scrapes static documentation into local artifacts, indexes those
artifacts in Qdrant, and searches them using hybrid dense and sparse retrieval
followed by reranking.

```text
docuwarden [global flags] <command> [command flags] [arguments]
```

The available commands are:

- `scrape`: crawl documentation and write an artifact.
- `index`: index an existing artifact in Qdrant.
- `ingest`: run `scrape` and `index` as one workflow.
- `sources`: list active indexed documentation sources and versions.
- `documents`: list indexed pages for a source and version.
- `search`: retrieve and rerank indexed documentation.
- `completion`: generate shell completion scripts.
- `help`: display command help.

Run `docuwarden <command> --help` for generated help.

## Configuration Precedence

Explicit command-line flags override environment-backed defaults. Environment
variables override built-in defaults. Secrets are accepted only through
environment variables and do not have corresponding flags.

The endpoint flags are base URLs. Do not append `/v1`; Docuwarden appends
`/v1/embeddings` or `/v1/rerank` itself.

## Global Flags

Global flags may appear before or after the subcommand.

| Flag | Environment variable | Default | Description |
| --- | --- | --- | --- |
| `--embedding-provider <name>` | `DOCUWARDEN_EMBEDDING_PROVIDER` | `openai` | Embedding adapter: `openai` or `voyage`. |
| `--embedding-endpoint <url>` | `DOCUWARDEN_EMBEDDING_ENDPOINT` | none | Embedding base URL. Required for `openai`; Voyage defaults to `https://api.voyageai.com`. |
| `--embedding-model <name>` | `DOCUWARDEN_EMBEDDING_MODEL` | none | Model sent in embedding requests. Required by `index`, `ingest`, and `search`. |
| `--reranker-provider <name>` | `DOCUWARDEN_RERANKER_PROVIDER` | `cohere` | Reranking adapter: `cohere` or `voyage`. |
| `--reranker-endpoint <url>` | `DOCUWARDEN_RERANKER_ENDPOINT` | none | Reranking base URL. Required for `cohere`; Voyage defaults to `https://api.voyageai.com`. |
| `--reranker-model <name>` | `DOCUWARDEN_RERANKER_MODEL` | none | Model sent in reranking requests. Required by `search`. |
| `--qdrant-host <host>` | `DOCUWARDEN_QDRANT_HOST` | `localhost` | Qdrant gRPC host. Used by `index`, `ingest`, and `search`. |
| `--qdrant-port <port>` | `DOCUWARDEN_QDRANT_PORT` | `6334` | Qdrant gRPC port. |
| `--qdrant-tls` | `DOCUWARDEN_QDRANT_TLS` | `false` | Connect to Qdrant using TLS. The environment value accepts Go boolean forms such as `true`, `false`, `1`, and `0`. |
| `--provider-timeout <duration>` | none | `1m` | Timeout for each embedding or reranking HTTP request. |
| `-h`, `--help` | none | false | Display help. |

Duration flags use Go duration syntax, for example `200ms`, `20s`, `5m`, or
`1h30m`.

## Secret Environment Variables

| Variable | Description |
| --- | --- |
| `DOCUWARDEN_EMBEDDING_API_KEY` | Sent as `Authorization: Bearer <key>` to the embedding endpoint. Optional for local unauthenticated servers. |
| `DOCUWARDEN_RERANKER_API_KEY` | Sent as `Authorization: Bearer <key>` to the reranker endpoint. Optional for local unauthenticated servers. |
| `VOYAGE_API_KEY` | Fallback credential for Voyage when the corresponding Docuwarden embedding or reranker key is unset. |
| `DOCUWARDEN_QDRANT_API_KEY` | Sent to Qdrant for authentication. Optional for the local Compose service. |

Example environment configuration for local `llama-server` instances:

```sh
export DOCUWARDEN_EMBEDDING_ENDPOINT=http://127.0.0.1:8080
export DOCUWARDEN_EMBEDDING_MODEL=qwen3-embedding-0.6b
export DOCUWARDEN_RERANKER_ENDPOINT=http://127.0.0.1:8081
export DOCUWARDEN_RERANKER_MODEL=qwen3-reranker-0.6b
export DOCUWARDEN_QDRANT_HOST=localhost
export DOCUWARDEN_QDRANT_PORT=6334
```

Example Voyage configuration:

```sh
export VOYAGE_API_KEY='<your secret key>'
export DOCUWARDEN_EMBEDDING_PROVIDER=voyage
export DOCUWARDEN_EMBEDDING_MODEL=voyage-4-large
export DOCUWARDEN_RERANKER_PROVIDER=voyage
export DOCUWARDEN_RERANKER_MODEL=rerank-2.5
```

Voyage endpoints default to `https://api.voyageai.com`. Indexing sends
`input_type=document`, while search query embedding sends `input_type=query`.

## `scrape`

```text
docuwarden scrape <url> --source <id> --content-selector <css> [flags]
```

Recursively crawls accepted documentation pages and writes a reusable artifact.
It does not contact embedding, reranking, or Qdrant services.

### Flags

| Flag | Default | Required | Description |
| --- | --- | --- | --- |
| `--source <id>` | none | yes | Stable source identifier, for example `nuxt`. Used in document IDs and later Qdrant names. |
| `--display-name <name>` | empty | no | Human-readable source name exposed by `sources`. |
| `--description <text>` | empty | no | Short source description exposed by `sources`. |
| `--tag <tag>` | empty | no | Repeatable technology tag exposed by `sources`. |
| `--version <version>` | empty | no | Version metadata, for example `4.x`. It does not modify the seed URL. |
| `--content-selector <css>` | none | yes | CSS selector identifying the documentation content. The first match is converted to Markdown. |
| `--output <dir>` | `artifacts/<source>/<version>` | no | Artifact directory. An empty version uses `artifacts/<source>/unversioned`. Existing `documents/` content is replaced. |
| `--workers <count>` | `4` | no | Maximum concurrent crawl workers. Must be positive. |
| `--throttle <duration>` | `100ms` | no | Minimum delay between requests to the same host. Use `0` to disable throttling. |
| `--request-timeout <duration>` | `20s` | no | Timeout for each crawl HTTP request. |
| `--retries <count>` | `3` | no | Retry count for network failures, HTTP `429`, and HTTP `5xx`. Must be non-negative. Permanent HTTP errors are not retried. |
| `--retry-backoff <duration>` | `200ms` | no | Initial exponential retry delay. |

Every `a[href]` in successfully parsed HTML is discovered automatically,
including links on pages whose content selector or Markdown conversion fails.
Discovered URLs must stay within the normalized seed origin and path. For a seed
of `https://nuxt.com/docs/4.x`, `/docs/4.x/api` is accepted while `/docs/5.x`,
`/docs/4.x-old`, and other origins are skipped. Redirects outside the boundary
are also skipped.

The artifact contains:

```text
<artifact-dir>/manifest.json
<artifact-dir>/report.json
<artifact-dir>/documents/*.md
```

Failed pages or missing content selectors mark the artifact incomplete and
produce a nonzero exit code, while successful documents and diagnostics remain
on disk.

## `retry`

```text
docuwarden retry <artifact-dir> [flags]
```

Retries every failed and selector-missing URL in an existing artifact. Existing
documents are preserved, repaired pages may discover new in-scope pages, and the
artifact is updated atomically in place. Retry does not index or publish the
artifact.

`--content-selector` is a repeatable addition to the selectors stored in the
artifact. Content selectors are tried in stored order, using the first match.
The crawl flags `--workers`, `--throttle`,
`--request-timeout`, `--retries`, and `--retry-backoff` override the stored
settings only when explicitly supplied.

The command exits nonzero while any failed or selector-missing pages remain. A
complete artifact is a successful no-op.

```sh
docuwarden retry artifacts/nuxt/4.x \
  --content-selector 'article.docs-content'
```

### Examples

Scrape one page:

```sh
./docuwarden scrape 'https://example.com/docs' \
  --source example \
  --content-selector main \
  --output artifacts/example
```

Recursively scrape Nuxt 4.x by following all in-scope anchors:

```sh
./docuwarden scrape 'https://nuxt.com/docs/4.x' \
  --source nuxt \
  --version 4.x \
  --content-selector 'main article' \
  --output artifacts/nuxt/4.x
```

## `index`

```text
docuwarden index <artifact-dir> [flags]
```

Reads an artifact, creates heading-aware Markdown chunks, generates dense and
sparse vectors, writes a new physical Qdrant collection, validates it, and then
atomically updates stable aliases.

### Flags

| Flag | Default | Description |
| --- | --- | --- |
| `--allow-incomplete` | false | Permit publication of an artifact whose manifest has `complete: false`. The override is recorded in point payloads. |
| `--embedding-batch-size <count>` | `64` | Number of chunk texts sent in each embedding HTTP request. Values at or below zero use `64`. Use `1` for conservative local llama.cpp operation. |
| `--snapshot-retention <count>` | `2` | Target number of recent physical collections retained per source. Active aliased collections are never deleted. Values at or below zero use `2`. |

Embedding input includes the page title, heading path, URL, and Markdown. Each
point stores named `dense` and `sparse` vectors. Qdrant applies IDF weighting to
the sparse vector.

Physical collections are named from the source and version:

```text
<source>__<version>__snapshot_<timestamp>_<suffix>
```

For example:

```text
nuxt__4_x__snapshot_1781300000000000000_a1b2c3d4
```

The unique suffix permits atomic replacement. A failed indexing run does not
switch the existing aliases.

### Examples

Index a complete Nuxt artifact with local llama.cpp:

```sh
./docuwarden index artifacts/nuxt/4.x \
  --embedding-batch-size 1 \
  --provider-timeout 10m
```

Explicitly publish successful pages from an incomplete artifact:

```sh
./docuwarden index artifacts/example \
  --allow-incomplete \
  --embedding-batch-size 1
```


## `ingest`

```text
docuwarden ingest <url> --source <id> --content-selector <css> [flags]
```

Runs `scrape` followed by `index`. The generated artifact is always retained.
It accepts every `scrape` flag plus the following indexing flags:

Workflow progress is written to stderr with timestamps. Crawl fetching is
reported in 10% increments using the pages discovered so far, followed by
artifact creation, chunking, embedding batches, vector store publication, and
completion. Stdout remains available for structured command output.

| Flag | Default | Description |
| --- | --- | --- |
| `--allow-incomplete` | false | Continue to indexing after an incomplete crawl and publish successful pages. |
| `--embedding-batch-size <count>` | `64` | Number of texts per embedding request. Values at or below zero use `64`. |
| `--snapshot-retention <count>` | `2` | Target physical collection retention count. Values at or below zero use `2`. |

Without `--allow-incomplete`, an incomplete crawl stops before publication. If
embedding or Qdrant publication fails, the artifact remains available and the
previous active index remains unchanged.

### Example

```sh
./docuwarden ingest 'https://nuxt.com/docs/4.x' \
  --source nuxt \
  --version 4.x \
  --content-selector 'main article' \
  --output artifacts/nuxt/4.x \
  --workers 4 \
  --throttle 100ms \
  --embedding-batch-size 1 \
  --provider-timeout 10m
```

If indexing fails after a successful crawl, restart the required service and
resume without recrawling:

```sh
./docuwarden index artifacts/nuxt/4.x \
  --embedding-batch-size 1 \
  --provider-timeout 10m
```

## `sources`

```text
docuwarden sources [--format json|text]
```

Lists active Docuwarden indexes from Qdrant. JSON output has a stable
`schema_version` and `sources` array. Each source contains its source ID,
optional display metadata and tags, default version, and active versions.
Version entries include seed URL, document and chunk counts, indexing time,
crawl completeness, and embedding model when recorded.

```sh
./docuwarden sources --format json
```

## `documents`

```text
docuwarden documents --source <id> [--version <version>] [--format json|text]
```

Lists the unique indexed pages for one source. Without `--version`, the source
default alias is used. JSON output includes the resolved version and a stable
`documents` array containing URL, title, and crawl time.

```sh
./docuwarden documents --source nuxt --version 4.x --format json
```

## `search`

```text
docuwarden search <query> --source <id> [flags]
```

Embeds and sparsely encodes the query, retrieves Qdrant candidates, reranks
them, suppresses near-duplicate overlapping chunks, and writes the selected
format to stdout.

### Flags

| Flag | Default | Required | Description |
| --- | --- | --- | --- |
| `--source <id>` | none | yes | Source to search. |
| `--version <version>` | empty | no | Search exactly this version. Without it, the source-default alias selects the most recently published successful version. |
| `--limit <count>` | `5` | no | Maximum final results after reranking and deduplication. |
| `--candidates <count>` | `40` | no | Candidates retrieved and passed to the reranker. It is automatically raised to at least `--limit`. |
| `--search-mode <mode>` | `hybrid` | no | `hybrid` uses dense plus sparse retrieval and Reciprocal Rank Fusion. `dense` uses only dense semantic retrieval. |
| `--format <format>` | `json` | no | Output format: `json` or `text`. |

Hybrid mode requires an index created by a current Docuwarden version. If a
legacy dense-only collection is active, re-run `index` for that artifact.

### JSON Output

JSON output has a stable top-level `results` array. Each result includes:

| Field | Description |
| --- | --- |
| `rank` | Final one-based reranked position. |
| `vector_score` | Compatibility score: fusion score in hybrid mode or dense score in dense mode. |
| `dense_score` | Qdrant cosine similarity from dense retrieval. |
| `sparse_score` | Qdrant sparse score with IDF weighting. |
| `fusion_score` | Qdrant Reciprocal Rank Fusion score. Zero in dense mode. |
| `reranker_score` | Score returned by the configured reranker. |
| `source` | Source ID. |
| `version` | Documentation version, omitted when empty. |
| `url` | Original page URL. |
| `title` | Page title, omitted when empty. |
| `heading_path` | Heading hierarchy, omitted when empty. |
| `markdown` | Original retrieved Markdown chunk. |

Example:

```sh
./docuwarden search 'useRuntimeConfig environment variables' \
  --source nuxt \
  --version 4.x \
  --format json \
  --limit 5 \
  --candidates 40 \
  --provider-timeout 10m
```

### Text Output

Text output is a compact Markdown context bundle containing result rank, title,
source URL, version, heading provenance, and content. Empty results print a
clear non-error message.

```sh
./docuwarden search 'How do I create a server API route?' \
  --source nuxt \
  --format text \
  --limit 5
```

Compare hybrid and dense retrieval:

```sh
./docuwarden search 'NUXT_PUBLIC_API_BASE' \
  --source nuxt \
  --search-mode hybrid \
  --format json

./docuwarden search 'NUXT_PUBLIC_API_BASE' \
  --source nuxt \
  --search-mode dense \
  --format json
```

## Shell Completion

Cobra provides completion generation for supported shells:

```sh
./docuwarden completion bash
./docuwarden completion fish
./docuwarden completion powershell
./docuwarden completion zsh
```

Each shell generator accepts:

| Flag | Default | Description |
| --- | --- | --- |
| `--no-descriptions` | false | Generate completion entries without command and flag descriptions. |
| `-h`, `--help` | false | Display help for that shell generator. |

For a temporary zsh session:

```sh
source <(./docuwarden completion zsh)
```

Persistent zsh completion on macOS:

```sh
./docuwarden completion zsh > "$(brew --prefix)/share/zsh/site-functions/_docuwarden"
```

Persistent fish completion:

```sh
./docuwarden completion fish > ~/.config/fish/completions/docuwarden.fish
```

## Output, Logs, And Exit Status

- `search` writes only the selected result format to stdout.
- Diagnostics, errors, and the successful `scrape` artifact path go to stderr.
- Successful commands exit with status `0`.
- Validation, crawl, provider, Qdrant, and cancellation failures exit with
  status `1`.
- `SIGINT` and `SIGTERM` cancel outstanding crawl, provider, and Qdrant
  operations. Alias publication does not occur unless indexing and validation
  complete successfully.

Examples for scripting:

```sh
# Capture machine-readable search results without diagnostics.
./docuwarden search 'defineNuxtPlugin' --source nuxt --format json >results.json

# Keep diagnostics separately.
./docuwarden search 'defineNuxtPlugin' --source nuxt --format json \
  >results.json 2>search.log

# Detect an incomplete or failed crawl.
if ! ./docuwarden scrape 'https://example.com/docs' \
  --source example --content-selector main; then
  jq '.failed, .selector_missing' artifacts/example/unversioned/report.json
fi
```
