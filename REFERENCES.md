# yfinance

```sh
./bin/docuwarden ingest 'https://ranaroussi.github.io/yfinance/' \
  --source yfinance \
  --display-name "yfinance" \
  --description "yfinance - Yahoo Finance wrapper" \
  --version 1.4.1 \
  --content-selector '#main-content > div > div.bd-article-container > article' \
  --output artifacts/yfinance/1.4.1 \
  --embedding-batch-size 64 \
  --provider-timeout 2m
```

# Nuxt

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

# Godot

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