# Docuwarden

Docuwarden crawls static documentation, stores reusable Markdown artifacts,
indexes heading-aware chunks in Qdrant, and retrieves reranked context for
coding agents.

See [CLI.md](CLI.md) for the complete command, flag, environment-variable, and
output reference.

## Table Of Contents

- [Major Features](#major-features)
- [Agent Skill](#agent-skill)
- [Quick Starts](#quick-starts)
- [Cloud Quick Start](#cloud-quick-start)
- [Local Quick Start](#local-quick-start)
- [Ingest Documentation](#ingest-documentation)
- [Recover From A Failed Ingest](#recover-from-a-failed-ingest)
- [Search](#search)
- [Retrieve A Complete Page](#retrieve-a-complete-page)
- [Validate The Index](#validate-the-index)
- [Configuration](#configuration)
- [Development](#development)

## Major Features

Docuwarden turns a documentation site into a reliable, searchable reference
for developers and coding agents:

1. **Crawl the documentation you care about.** Point Docuwarden at a versioned
   documentation path and it follows in-scope pages, extracts the useful
   content, and converts it to clean Markdown.
2. **Keep a reusable local copy.** The crawl is saved as Markdown plus a report,
   so you can inspect the result, retry only failed pages, or rebuild the index
   without downloading the entire site again.
3. **Publish a searchable version.** Docuwarden splits pages along their heading
   structure and indexes both focused sections and complete pages in Qdrant.
   Updating an index does not replace the working version until the new one is
   ready.
4. **Find the right context for a task.** Search matches both concepts and exact
   technical terms, reranks the candidates, removes repetitive results, and
   returns concise Markdown or JSON that can be placed directly into a prompt.
5. **Retrieve authoritative pages when the URL is known.** Developers and agents
   can list available sources, discover indexed pages, or fetch a complete page
   without relying on search snippets or a live website.

The model and storage stack can run locally with OpenAI- and Cohere-compatible
servers and Qdrant, or use Voyage AI and a hosted Qdrant instance. The bundled
agent skill makes documentation-first research the default workflow for coding
agents.

## Agent Skill

The bundled [Docuwarden documentation search skill](skills/docuwarden-docs/SKILL.md)
makes coding agents consult your indexed documentation before falling back to
the web. This gives them stable, version-specific context instead of relying on
search snippets, whatever documentation happens to be current, or pages that
may change during a task.

With the skill, an agent can:

- Discover which documentation sources, versions, and pages are available.
- Search by both concepts and exact API names.
- Receive focused, deduplicated Markdown that is ready to use as context.
- Retrieve a complete authoritative page when its URL is known.

This reduces stale-version answers, irrelevant context, and time spent
navigating documentation sites. The skill is intentionally read-only: it lets
agents list, search, and retrieve documentation, but not scrape, ingest, or
replace indexes.

## Quick Starts

Choose the setup that matches where you want to run the model and vector
services:

- [Cloud quick start](#cloud-quick-start): Voyage AI for embeddings and
  reranking, with Qdrant Cloud for vector storage.
- [Local quick start](#local-quick-start): local Qwen models served by
  `llama-server`, with Qdrant running under Podman Compose.

Both setups use `./bin/docuwarden`. Build it first with `make build`; build and
test details are in [Development](#development).

## Cloud Quick Start

Create an account with [Voyage AI](https://www.voyageai.com/) for an API key
and with [Qdrant](https://qdrant.tech/) for a Qdrant Cloud cluster. Then
configure Docuwarden with the cluster's gRPC host and API key:

```sh
export VOYAGE_API_KEY=''
export DOCUWARDEN_EMBEDDING_PROVIDER=voyage
export DOCUWARDEN_EMBEDDING_MODEL=voyage-4-lite
export DOCUWARDEN_RERANKER_PROVIDER=voyage
export DOCUWARDEN_RERANKER_MODEL=rerank-2.5-lite
export DOCUWARDEN_QDRANT_HOST=''
export DOCUWARDEN_QDRANT_PORT=6334
export DOCUWARDEN_QDRANT_TLS=true
export DOCUWARDEN_QDRANT_API_KEY=''
```

Set `DOCUWARDEN_QDRANT_HOST` to the Qdrant cluster's bare gRPC hostname, without
an `https://` prefix. Docuwarden connects over gRPC and enables transport
security through `DOCUWARDEN_QDRANT_TLS=true`.

Voyage requests default to `https://api.voyageai.com`, so no embedding or
reranker endpoint is required. Continue with the shared
[ingest examples](#ingest-documentation).

## Local Quick Start

This setup runs Qdrant under Podman and serves local Qwen embedding and
reranking models with `llama-server` from
[llama.cpp](https://github.com/ggml-org/llama.cpp).

### Start Qdrant

```sh
podman compose up -d --wait qdrant
```

The Compose service exposes Qdrant's REST API and Web UI on port `6333` and the
gRPC API used by Docuwarden on port `6334`.

### Serve The Models

Download the
[Qwen3 embedding model](https://huggingface.co/Qwen/Qwen3-Embedding-0.6B-GGUF)
and run it in one terminal:

```sh
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

Download the
[Qwen3 reranker model](https://huggingface.co/ggml-org/Qwen3-Reranker-0.6B-Q8_0-GGUF)
and run it in a second terminal:

```sh
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

`--parallel 1` keeps each request in one server slot. The larger physical
batches allow documentation chunks and reranking inputs to exceed llama.cpp's
smaller default batch without terminating the server.

### Configure Docuwarden

Use endpoint base URLs without a `/v1` suffix:

```sh
export DOCUWARDEN_EMBEDDING_PROVIDER=openai
export DOCUWARDEN_EMBEDDING_ENDPOINT=http://127.0.0.1:8080
export DOCUWARDEN_EMBEDDING_MODEL=qwen3-embedding-0.6b
export DOCUWARDEN_RERANKER_PROVIDER=cohere
export DOCUWARDEN_RERANKER_ENDPOINT=http://127.0.0.1:8081
export DOCUWARDEN_RERANKER_MODEL=qwen3-reranker-0.6b
export DOCUWARDEN_QDRANT_HOST=localhost
export DOCUWARDEN_QDRANT_PORT=6334
export DOCUWARDEN_QDRANT_TLS=false
```

The local services do not require API keys. For conservative local embedding,
change `--embedding-batch-size 64` in the examples below to `1` and increase
`--provider-timeout` if necessary.

Continue with the shared [ingest examples](#ingest-documentation).

## Ingest Documentation

`ingest` crawls the documentation, retains a reusable artifact, and publishes
the resulting chunks to Qdrant. The crawler follows in-scope links under the
seed URL's origin and path prefix.

### Nuxt

```sh
./bin/docuwarden ingest 'https://nuxt.com/docs/4.x' \
  --source nuxt \
  --display-name "Nuxt" \
  --description "Nuxt framework documentation" \
  --version 4.x \
  --content-selector '#__nuxt > div.flex > div.flex-1.min-w-0 > div > main > div > div > div > div > div.lg\:col-span-9' \
  --output artifacts/nuxt/4.x \
  --embedding-batch-size 64 \
  --provider-timeout 2m
```

### Godot

```sh
./bin/docuwarden ingest 'https://docs.godotengine.org/en/stable' \
  --source godot \
  --display-name "Godot" \
  --description "Godot game engine documentation" \
  --version 4.6 \
  --content-selector 'body > div.wy-grid-for-nav > section > div > div > div.document > div' \
  --output artifacts/godot/4.6 \
  --embedding-batch-size 64 \
  --provider-timeout 2m
```

Each artifact contains:

```text
<artifact-dir>/manifest.json
<artifact-dir>/report.json
<artifact-dir>/documents/*.md
```

## Recover From A Failed Ingest

An ingest has separate crawl and indexing phases. The artifact is retained
when either phase fails, so recovery does not need to repeat successful work.

### Retry Failed Pages, Then Index

If the crawl failed for some pages or a content selector did not match, retry
only the failed and selector-missing URLs. `retry` updates the existing
artifact and does not publish it:

```sh
./bin/docuwarden retry artifacts/nuxt/4.x
```

You can add a fallback selector during recovery:

```sh
./bin/docuwarden retry artifacts/nuxt/4.x \
  --content-selector 'article.docs-content'
```

After retry succeeds, index the repaired artifact without crawling again:

```sh
./bin/docuwarden index artifacts/nuxt/4.x \
  --embedding-batch-size 64 \
  --provider-timeout 2m
```

### Index Only After An Indexing Failure

If crawling completed but embedding or Qdrant publication failed, restore the
failed service and run only `index`:

```sh
./bin/docuwarden index artifacts/nuxt/4.x \
  --embedding-batch-size 64 \
  --provider-timeout 2m
```

Check `manifest.json` before indexing. Its `complete` field must be `true`
unless you intentionally pass `--allow-incomplete`. A failed publication does
not replace the previously active Qdrant index.

## Search

List the available sources and documents:

```sh
./bin/docuwarden sources --format json
./bin/docuwarden documents --source nuxt --version 4.x --format json
```

Return a prompt-ready Markdown context bundle:

```sh
./bin/docuwarden search \
  'How do I define runtime configuration in Nuxt?' \
  --source nuxt \
  --version 4.x \
  --format text \
  --limit 5 \
  --candidates 40 \
  --provider-timeout 2m
```

Use `--format json` for machine-readable results. Search defaults to hybrid
dense and sparse retrieval followed by reranking. Omitting `--version` searches
the most recently indexed successful version for the source.

## Retrieve A Complete Page

Use the exact URL returned by `search` or `documents` to retrieve the original
stored Markdown. This does not contact the source website or invoke embedding
or reranking providers.

```sh
./bin/docuwarden get 'https://nuxt.com/docs/4.x/guide' \
  --source nuxt \
  --version 4.x
```

Omitting `--version` uses the source's default version. Collections created by
older Docuwarden versions must be re-indexed before they support `get`.

## Validate The Index

For local Qdrant, open <http://localhost:6333/dashboard>. In Qdrant Cloud, open
the cluster dashboard.

Confirm that:

1. A collection named like `<source>__<version>__snapshot_<timestamp>_<suffix>`
   exists and has a green status.
2. Its point count equals the searchable chunk count plus the document count.
3. The collection defines named `dense` and `sparse` vectors.
4. Point payloads include `point_kind` plus fields such as `source`, `version`, `url`, `title`,
   `heading_path`, `chunk_index`, `markdown`, `content_hash`, and `crawled_at`.

Docuwarden publishes a new physical collection before switching stable aliases,
so a failed replacement leaves the previous index active.

## Development

Development requires Go 1.26.

Build the CLI and run the unit tests:

```sh
make build
make test
```

Run the complete compiled-CLI workflow with the local Qdrant Compose service
and deterministic in-process model services:

```sh
make e2e
```

Override the Compose command when needed, for example:

```sh
make e2e COMPOSE='docker compose'
```
