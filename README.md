# Docuwarden

Docuwarden crawls static documentation, stores reusable Markdown artifacts, indexes heading-aware chunks in Qdrant, and retrieves reranked context for coding agents.

See [CLI.md](CLI.md) for the complete command, flag, environment-variable, and
output reference.

## Build And Test

Requires Go 1.26.

```sh
make test
make build
```

The complete compiled-CLI workflow uses Podman Compose, a local Qdrant container, and deterministic in-process model services:

```sh
make e2e
```

Override the Compose command when needed, for example `make e2e COMPOSE='docker compose'`.

## Configuration

Secrets are environment-only:

```text
DOCUWARDEN_QDRANT_API_KEY
DOCUWARDEN_EMBEDDING_API_KEY
DOCUWARDEN_RERANKER_API_KEY
VOYAGE_API_KEY
```

Non-secret settings can use flags or these environment variables:

```text
DOCUWARDEN_QDRANT_HOST          default: localhost
DOCUWARDEN_QDRANT_PORT          default: 6334
DOCUWARDEN_QDRANT_TLS           default: false
DOCUWARDEN_EMBEDDING_ENDPOINT
DOCUWARDEN_EMBEDDING_PROVIDER   default: openai
DOCUWARDEN_EMBEDDING_MODEL
DOCUWARDEN_RERANKER_ENDPOINT
DOCUWARDEN_RERANKER_PROVIDER    default: cohere
DOCUWARDEN_RERANKER_MODEL
```

Voyage AI is supported directly for both operations:

```sh
export VOYAGE_API_KEY='<your secret key>'
export DOCUWARDEN_EMBEDDING_PROVIDER=voyage
export DOCUWARDEN_EMBEDDING_MODEL=voyage-4-lite
export DOCUWARDEN_RERANKER_PROVIDER=voyage
export DOCUWARDEN_RERANKER_MODEL=rerank-2.5-lite
```

The Voyage API endpoint defaults to `https://api.voyageai.com`. The
operation-specific Docuwarden API key variables override `VOYAGE_API_KEY`.

Incomplete crawl artifacts can be repaired in place without re-fetching
successful pages or publishing to Qdrant:

```sh
docuwarden retry artifacts/nuxt/4.x \
  --content-selector 'article.docs-content' \
  --link-selector '.docs-navigation a'
```

## Nuxt Quickstart

This example indexes the Nuxt 4.x documentation using Qdrant under Podman and
local Qwen embedding and reranking models served by `llama-server`.

### 1. Start The Models

Run each server in a separate terminal:

[Qwen3-Embedding-0.6B](https://huggingface.co/Qwen/Qwen3-Embedding-0.6B-GGUF)
```sh
# Embedding server
llama-server \
  -m ~/Downloads/Qwen3-Embedding-0.6B-Q8_0.gguf \
  --embedding \
  --parallel 1 \
  --batch-size 2048 \
  --ubatch-size 2048 \
  --cache-ram 0 \
  --ctx-checkpoints 0 \
  --no-cont-batching \
  --alias qwen3-embedding-0.6b \
  --host 127.0.0.1 \
  --port 8080
```

[qwen3-reranker-0.6b](https://huggingface.co/ggml-org/Qwen3-Reranker-0.6B-Q8_0-GGUF)
```sh
# Reranker server
llama-server \
  -m ~/Downloads/qwen3-reranker-0.6b-q8_0.gguf \
  --embedding \
  --pooling rank \
  --rerank \
  --parallel 1 \
  --batch-size 4096 \
  --ubatch-size 4096 \
  --cache-ram 0 \
  --ctx-checkpoints 0 \
  --no-cont-batching \
  --alias qwen3-reranker-0.6b \
  --host 127.0.0.1 \
  --port 8081
```

`--parallel 1` prevents `llama-server` from distributing one embedding request
across several slots. The matching `--batch-size 2048` and
`--ubatch-size 2048` settings allow one documentation chunk to contain up to
2,048 model tokens. Without them, llama.cpp currently reduces the physical
batch to 512 tokens, which can terminate the server when a larger Nuxt chunk is
embedded. Prompt caching, context checkpoints, and continuous batching are
disabled because independent embedding requests do not benefit from them.

`--embedding-batch-size 1` sends one chunk per HTTP request. This is slower than
larger request batches, but is the most conservative setting for local
llama.cpp embedding servers and does not change embedding quality.

The reranker uses the same single-slot and cache-disabled settings. Its
physical batch is 4,096 tokens because each reranking input combines the query,
model-specific formatting, and a retrieved documentation chunk.

### 2. Build Docuwarden And Start Qdrant

```sh
make build
podman compose up -d --wait qdrant
```

The Compose configuration pins Qdrant `v1.18.2`, the latest stable release.
It exposes the REST API and Web UI on port `6333`, and the gRPC API used by
Docuwarden on port `6334`.

### 3. Configure Docuwarden

Use endpoint base URLs without the `/v1` suffix:

```sh
export DOCUWARDEN_EMBEDDING_ENDPOINT=http://127.0.0.1:8080
export DOCUWARDEN_EMBEDDING_MODEL=qwen3-embedding-0.6b
export DOCUWARDEN_RERANKER_ENDPOINT=http://127.0.0.1:8081
export DOCUWARDEN_RERANKER_MODEL=qwen3-reranker-0.6b
```

These local servers do not require API keys.

### 4. Ingest Nuxt 4.x

The supplied selectors identify navigation containers, so `a[href]` is appended
to each link selector to select the links inside them.

```sh
./docuwarden ingest 'https://nuxt.com/docs/4.x' \
  --source nuxt \
  --version 4.x \
  --link-selector '#__nuxt > div.flex > div.flex-1.min-w-0 > div > main > div > div > aside > div a[href]' \
  --link-selector '#__nuxt > div.flex > div.flex-1.min-w-0 > div > header > div.w-full.max-w-\(--ui-container\).mx-auto.px-4.sm\:px-6.lg\:px-8.hidden.lg\:flex.items-center.justify-between a[href]' \
  --content-selector '#__nuxt > div.flex > div.flex-1.min-w-0 > div > main > div > div > div > div > div.lg\:col-span-9' \
  --output artifacts/nuxt/4.x \
  --embedding-batch-size 1 \
  --provider-timeout 10m
```

The retained artifact contains `manifest.json`, `report.json`, and deterministic
Markdown files under `artifacts/nuxt/4.x/documents/`.

Indexing stores two named vectors for every chunk:

- `dense`: semantic Qwen embedding of the title, heading path, URL, and content.
- `sparse`: lexical term frequencies with Qdrant's server-side IDF modifier.

### 5. Validate The Index In Qdrant

Open the Qdrant Web UI after ingestion:

<http://localhost:6333/dashboard>

Use the UI to validate the indexed data:

1. Open **Collections** and select a collection beginning with
   `nuxt__4_x__snapshot_`.
2. Confirm the collection status is green and the point count is greater than
   zero. Each point represents one Markdown chunk.
3. Open the collection's **Data** or points view and inspect several points.
4. Confirm the collection has named `dense` and `sparse` vector definitions.
5. Confirm point payloads contain `source: "nuxt"`, `version: "4.x"`, `url`,
   `title`, `heading_path`, `chunk_index`, `markdown`, `content_hash`, and
   `crawled_at`.
6. Check that the stored Markdown and URL correspond to the Nuxt page from
   which the chunk was produced.

Docuwarden uses the source ID and version in physical collection names. For
`--source nuxt --version 4.x`, names use the format
`nuxt__4_x__snapshot_<timestamp>_<suffix>`. The unique suffix is required for
atomic replacement: a new collection is fully indexed before stable aliases
are switched to it. Search uses those aliases rather than writing directly into
the active collection.

The Web UI **Console** can also validate the service and list collections:

```http
GET /healthz
GET /collections
GET /aliases
```

For the official UI documentation, see
<https://qdrant.tech/documentation/web-ui/>.

### 6. Search

Discover the documentation currently available to search:

```sh
./docuwarden sources --format json
./docuwarden documents --source nuxt --version 4.x --format json
```

`sources` reads active Qdrant aliases, so its catalog stays synchronized with
atomic index publication. New indexes include source metadata, document and
chunk counts, crawl completeness, indexing time, and the embedding model.

Return a prompt-ready Markdown context bundle:

```sh
./docuwarden search \
  'How do I define runtime configuration in Nuxt?' \
  --source nuxt \
  --version 4.x \
  --format text \
  --limit 5 \
  --candidates 40 \
  --provider-timeout 10m
```

Return machine-readable JSON:

```sh
./docuwarden search \
  'How do I create a server API route?' \
  --source nuxt \
  --format json \
  --limit 5 \
  --candidates 40 \
  --provider-timeout 10m
```

Search defaults to hybrid retrieval. Qdrant retrieves dense and sparse
candidates, combines them with Reciprocal Rank Fusion, and Docuwarden reranks
the fused top 40 with the configured reranker. Near-duplicate overlapping
chunks from the same page are suppressed before the top results are returned.

JSON results include `dense_score`, `sparse_score`, `fusion_score`, and
`reranker_score`. The compatibility field `vector_score` contains the fusion
score. Use `--search-mode dense` only for troubleshooting or comparison.

Omitting `--version` searches the most recently indexed successful version for
the source. Incomplete crawls remain on disk and are not published unless
`--allow-incomplete` is explicitly supplied. Each indexing run publishes a new
physical Qdrant collection and updates stable aliases only after validation.

### Resume After An Embedding Server Failure

Scraping and indexing are separate phases even when invoked through `ingest`.
If the crawl artifact is complete but the embedding server stops, restart the
server and index the retained artifact without crawling the site again:

```sh
./docuwarden index artifacts/nuxt/4.x \
  --embedding-batch-size 1 \
  --provider-timeout 10m
```

Check `artifacts/nuxt/4.x/manifest.json` first. Its `complete` field must be
`true` unless incomplete indexing is intentionally enabled.
