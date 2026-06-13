package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/zero/docuwarden/app"
	"github.com/zero/docuwarden/corpus"
	"github.com/zero/docuwarden/embedding"
	"github.com/zero/docuwarden/rerank"
	"github.com/zero/docuwarden/retrieval"
	"github.com/zero/docuwarden/scrape"
	"github.com/zero/docuwarden/vectorstore"
	qdrantstore "github.com/zero/docuwarden/vectorstore/qdrant"
)

type providerFlags struct {
	embeddingProvider string
	embeddingEndpoint string
	embeddingModel    string
	rerankerProvider  string
	rerankerEndpoint  string
	rerankerModel     string
	qdrantHost        string
	qdrantPort        int
	qdrantTLS         bool
	timeout           time.Duration
}

type scrapeFlags struct {
	source          string
	displayName     string
	description     string
	tags            []string
	version         string
	linkSelectors   []string
	contentSelector string
	output          string
	workers         int
	throttle        time.Duration
	requestTimeout  time.Duration
	retries         int
	backoff         time.Duration
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := newRoot(os.Stdout, os.Stderr).ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "docuwarden:", err)
		os.Exit(1)
	}
}

func newRoot(stdout, stderr io.Writer) *cobra.Command {
	var providers providerFlags
	root := &cobra.Command{Use: "docuwarden", Short: "Scrape, index, and retrieve static documentation", SilenceErrors: true, SilenceUsage: true}
	root.SetOut(stdout)
	root.SetErr(stderr)
	providers.add(root)
	root.AddCommand(newScrapeCommand(&providers), newIndexCommand(&providers), newIngestCommand(&providers), newSearchCommand(&providers), newSourcesCommand(&providers), newDocumentsCommand(&providers))
	return root
}

func (flags *providerFlags) add(command *cobra.Command) {
	command.PersistentFlags().StringVar(&flags.embeddingProvider, "embedding-provider", env("DOCUWARDEN_EMBEDDING_PROVIDER", "openai"), "embedding provider: openai or voyage")
	command.PersistentFlags().StringVar(&flags.embeddingEndpoint, "embedding-endpoint", env("DOCUWARDEN_EMBEDDING_ENDPOINT", ""), "embedding endpoint base URL")
	command.PersistentFlags().StringVar(&flags.embeddingModel, "embedding-model", env("DOCUWARDEN_EMBEDDING_MODEL", ""), "embedding model")
	command.PersistentFlags().StringVar(&flags.rerankerProvider, "reranker-provider", env("DOCUWARDEN_RERANKER_PROVIDER", "cohere"), "reranker provider: cohere or voyage")
	command.PersistentFlags().StringVar(&flags.rerankerEndpoint, "reranker-endpoint", env("DOCUWARDEN_RERANKER_ENDPOINT", ""), "reranker endpoint base URL")
	command.PersistentFlags().StringVar(&flags.rerankerModel, "reranker-model", env("DOCUWARDEN_RERANKER_MODEL", ""), "reranker model")
	command.PersistentFlags().StringVar(&flags.qdrantHost, "qdrant-host", env("DOCUWARDEN_QDRANT_HOST", "localhost"), "Qdrant gRPC host")
	command.PersistentFlags().IntVar(&flags.qdrantPort, "qdrant-port", envInt("DOCUWARDEN_QDRANT_PORT", 6334), "Qdrant gRPC port")
	command.PersistentFlags().BoolVar(&flags.qdrantTLS, "qdrant-tls", envBool("DOCUWARDEN_QDRANT_TLS", false), "use TLS for Qdrant")
	command.PersistentFlags().DurationVar(&flags.timeout, "provider-timeout", 60*time.Second, "model provider request timeout")
}

func newScrapeCommand(providers *providerFlags) *cobra.Command {
	var flags scrapeFlags
	command := &cobra.Command{Use: "scrape <url>", Args: cobra.ExactArgs(1), RunE: func(command *cobra.Command, args []string) error {
		if err := flags.validate(); err != nil {
			return err
		}
		output := flags.outputPath()
		_, err := app.Scrape(command.Context(), flags.config(args[0]), output)
		if err == nil {
			fmt.Fprintln(command.ErrOrStderr(), "artifact:", output)
		}
		return err
	}}
	flags.add(command)
	return command
}

func newIndexCommand(providers *providerFlags) *cobra.Command {
	var allowIncomplete bool
	var retention, batchSize int
	command := &cobra.Command{Use: "index <artifact-dir>", Args: cobra.ExactArgs(1), RunE: func(command *cobra.Command, args []string) error {
		service, closeStore, err := providers.indexService()
		if err != nil {
			return err
		}
		defer closeStore()
		return service.Index(command.Context(), args[0], app.IndexOptions{AllowIncomplete: allowIncomplete, Retention: retention, BatchSize: batchSize, EmbeddingModel: providers.embeddingModel})
	}}
	command.Flags().BoolVar(&allowIncomplete, "allow-incomplete", false, "publish an incomplete crawl artifact")
	command.Flags().IntVar(&retention, "snapshot-retention", 2, "number of recent physical snapshots to retain")
	command.Flags().IntVar(&batchSize, "embedding-batch-size", 64, "texts per embedding request")
	return command
}

func newIngestCommand(providers *providerFlags) *cobra.Command {
	var flags scrapeFlags
	var allowIncomplete bool
	var retention, batchSize int
	command := &cobra.Command{Use: "ingest <url>", Args: cobra.ExactArgs(1), RunE: func(command *cobra.Command, args []string) error {
		if err := flags.validate(); err != nil {
			return err
		}
		service, closeStore, err := providers.indexService()
		if err != nil {
			return err
		}
		defer closeStore()
		return service.Ingest(command.Context(), flags.config(args[0]), flags.outputPath(), app.IndexOptions{AllowIncomplete: allowIncomplete, Retention: retention, BatchSize: batchSize, EmbeddingModel: providers.embeddingModel})
	}}
	flags.add(command)
	command.Flags().BoolVar(&allowIncomplete, "allow-incomplete", false, "publish successful pages from an incomplete crawl")
	command.Flags().IntVar(&retention, "snapshot-retention", 2, "number of recent physical snapshots to retain")
	command.Flags().IntVar(&batchSize, "embedding-batch-size", 64, "texts per embedding request")
	return command
}

func newSourcesCommand(providers *providerFlags) *cobra.Command {
	var format string
	command := &cobra.Command{Use: "sources", Short: "List indexed documentation sources and versions", Args: cobra.NoArgs, RunE: func(command *cobra.Command, _ []string) error {
		if format != "json" && format != "text" {
			return fmt.Errorf("--format must be json or text")
		}
		store, err := providers.store()
		if err != nil {
			return err
		}
		defer store.Close()
		catalog, err := store.ListSources(command.Context())
		if err != nil {
			return err
		}
		return writeCatalog(command.OutOrStdout(), catalog, format)
	}}
	command.Flags().StringVar(&format, "format", "json", "output format: json or text")
	return command
}

func newDocumentsCommand(providers *providerFlags) *cobra.Command {
	var source, version, format string
	command := &cobra.Command{Use: "documents", Short: "List indexed pages for a documentation source", Args: cobra.NoArgs, RunE: func(command *cobra.Command, _ []string) error {
		if source == "" {
			return fmt.Errorf("--source is required")
		}
		if format != "json" && format != "text" {
			return fmt.Errorf("--format must be json or text")
		}
		store, err := providers.store()
		if err != nil {
			return err
		}
		defer store.Close()
		documents, err := store.ListDocuments(command.Context(), source, version)
		if err != nil {
			return err
		}
		return writeDocuments(command.OutOrStdout(), documents, format)
	}}
	command.Flags().StringVar(&source, "source", "", "source ID")
	command.Flags().StringVar(&version, "version", "", "exact documentation version")
	command.Flags().StringVar(&format, "format", "json", "output format: json or text")
	return command
}

func writeCatalog(writer io.Writer, catalog vectorstore.Catalog, format string) error {
	if format == "json" {
		encoder := json.NewEncoder(writer)
		encoder.SetIndent("", "  ")
		return encoder.Encode(catalog)
	}
	if len(catalog.Sources) == 0 {
		_, err := fmt.Fprintln(writer, "No indexed documentation sources.")
		return err
	}
	for _, source := range catalog.Sources {
		name := source.DisplayName
		if name == "" {
			name = source.Source
		}
		if _, err := fmt.Fprintf(writer, "%s (source: %s)\n", name, source.Source); err != nil {
			return err
		}
		for _, version := range source.Versions {
			label := version.Version
			if label == "" {
				label = "unversioned"
			}
			marker := ""
			if version.Version == source.DefaultVersion {
				marker = " [default]"
			}
			if _, err := fmt.Fprintf(writer, "  %s%s: %d documents, %d chunks\n", label, marker, version.DocumentCount, version.ChunkCount); err != nil {
				return err
			}
		}
	}
	return nil
}

func writeDocuments(writer io.Writer, catalog vectorstore.DocumentCatalog, format string) error {
	if format == "json" {
		encoder := json.NewEncoder(writer)
		encoder.SetIndent("", "  ")
		return encoder.Encode(catalog)
	}
	if len(catalog.Documents) == 0 {
		_, err := fmt.Fprintln(writer, "No indexed documents.")
		return err
	}
	for _, document := range catalog.Documents {
		if document.Title == "" {
			if _, err := fmt.Fprintln(writer, document.URL); err != nil {
				return err
			}
			continue
		}
		if _, err := fmt.Fprintf(writer, "%s\n  %s\n", document.Title, document.URL); err != nil {
			return err
		}
	}
	return nil
}

func newSearchCommand(providers *providerFlags) *cobra.Command {
	var source, version, format, searchMode string
	var limit, candidates int
	command := &cobra.Command{Use: "search <query>", Args: cobra.ExactArgs(1), RunE: func(command *cobra.Command, args []string) error {
		if source == "" {
			return fmt.Errorf("--source is required")
		}
		if format != "json" && format != "text" {
			return fmt.Errorf("--format must be json or text")
		}
		if searchMode != string(vectorstore.SearchModeHybrid) && searchMode != string(vectorstore.SearchModeDense) {
			return fmt.Errorf("--search-mode must be hybrid or dense")
		}
		client := &http.Client{Timeout: providers.timeout}
		embedder, err := providers.embedder(client, "query")
		if err != nil {
			return err
		}
		reranker, err := providers.reranker(client)
		if err != nil {
			return err
		}
		store, err := providers.store()
		if err != nil {
			return err
		}
		defer store.Close()
		service := retrieval.Service{Embedder: embedder, Reranker: reranker, Store: store, Candidates: candidates, Mode: vectorstore.SearchMode(searchMode)}
		results, err := service.Search(command.Context(), args[0], source, version, limit)
		if err != nil {
			return err
		}
		if format == "json" {
			return retrieval.WriteJSON(command.OutOrStdout(), results)
		}
		return retrieval.WriteText(command.OutOrStdout(), results)
	}}
	command.Flags().StringVar(&source, "source", "", "source ID")
	command.Flags().StringVar(&version, "version", "", "exact documentation version")
	command.Flags().IntVar(&limit, "limit", 5, "number of final results")
	command.Flags().IntVar(&candidates, "candidates", 40, "hybrid candidates to rerank")
	command.Flags().StringVar(&searchMode, "search-mode", string(vectorstore.SearchModeHybrid), "retrieval mode: hybrid or dense")
	command.Flags().StringVar(&format, "format", "json", "output format: json or text")
	return command
}

func (flags *scrapeFlags) add(command *cobra.Command) {
	command.Flags().StringVar(&flags.source, "source", "", "source ID")
	command.Flags().StringVar(&flags.displayName, "display-name", "", "human-readable source name")
	command.Flags().StringVar(&flags.description, "description", "", "short source description")
	command.Flags().StringSliceVar(&flags.tags, "tag", nil, "repeatable source technology tag")
	command.Flags().StringVar(&flags.version, "version", "", "documentation version metadata")
	command.Flags().StringSliceVar(&flags.linkSelectors, "link-selector", nil, "repeatable CSS selector for crawl links")
	command.Flags().StringVar(&flags.contentSelector, "content-selector", "", "CSS selector for page content")
	command.Flags().StringVar(&flags.output, "output", "", "artifact output directory")
	command.Flags().IntVar(&flags.workers, "workers", 4, "concurrent crawl workers")
	command.Flags().DurationVar(&flags.throttle, "throttle", 100*time.Millisecond, "minimum delay between requests to one host")
	command.Flags().DurationVar(&flags.requestTimeout, "request-timeout", 20*time.Second, "HTTP request timeout")
	command.Flags().IntVar(&flags.retries, "retries", 3, "transient request retries")
	command.Flags().DurationVar(&flags.backoff, "retry-backoff", 200*time.Millisecond, "initial retry backoff")
}

func (flags scrapeFlags) validate() error {
	if flags.source == "" {
		return fmt.Errorf("--source is required")
	}
	if flags.contentSelector == "" {
		return fmt.Errorf("--content-selector is required")
	}
	if flags.workers <= 0 || flags.retries < 0 {
		return fmt.Errorf("workers must be positive and retries non-negative")
	}
	return nil
}

func (flags scrapeFlags) config(seed string) scrape.Config {
	return scrape.Config{Source: corpus.SourceSpec{SourceID: flags.source, DisplayName: flags.displayName, Description: flags.description, Tags: flags.tags, SeedURL: seed, LinkSelectors: flags.linkSelectors, ContentSelector: flags.contentSelector, Version: flags.version}, Workers: flags.workers, Throttle: flags.throttle, Timeout: flags.requestTimeout, MaxRetries: flags.retries, Backoff: flags.backoff}
}

func (flags scrapeFlags) outputPath() string {
	if flags.output != "" {
		return flags.output
	}
	version := flags.version
	if version == "" {
		version = "unversioned"
	}
	return filepath.Join("artifacts", safePath(flags.source), safePath(version))
}

func (flags providerFlags) indexService() (app.Service, func(), error) {
	client := &http.Client{Timeout: flags.timeout}
	embedder, err := flags.embedder(client, "document")
	if err != nil {
		return app.Service{}, func() {}, err
	}
	store, err := flags.store()
	if err != nil {
		return app.Service{}, func() {}, err
	}
	return app.Service{Embedder: embedder, Store: store}, func() { _ = store.Close() }, nil
}

func (flags providerFlags) embedder(client *http.Client, inputType string) (embedding.Embedder, error) {
	if flags.embeddingModel == "" {
		return nil, fmt.Errorf("embedding model is required")
	}
	switch flags.embeddingProvider {
	case "openai":
		if flags.embeddingEndpoint == "" {
			return nil, fmt.Errorf("embedding endpoint is required for provider openai")
		}
		return embedding.OpenAI{Endpoint: flags.embeddingEndpoint, Model: flags.embeddingModel, APIKey: os.Getenv("DOCUWARDEN_EMBEDDING_API_KEY"), Client: client}, nil
	case "voyage":
		endpoint := flags.embeddingEndpoint
		if endpoint == "" {
			endpoint = "https://api.voyageai.com"
		}
		return embedding.Voyage{Endpoint: endpoint, Model: flags.embeddingModel, APIKey: providerAPIKey("DOCUWARDEN_EMBEDDING_API_KEY"), InputType: inputType, Client: client}, nil
	default:
		return nil, fmt.Errorf("unsupported embedding provider %q", flags.embeddingProvider)
	}
}

func (flags providerFlags) reranker(client *http.Client) (rerank.Reranker, error) {
	if flags.rerankerModel == "" {
		return nil, fmt.Errorf("reranker model is required")
	}
	switch flags.rerankerProvider {
	case "cohere":
		if flags.rerankerEndpoint == "" {
			return nil, fmt.Errorf("reranker endpoint is required for provider cohere")
		}
		return rerank.Cohere{Endpoint: flags.rerankerEndpoint, Model: flags.rerankerModel, APIKey: os.Getenv("DOCUWARDEN_RERANKER_API_KEY"), Client: client}, nil
	case "voyage":
		endpoint := flags.rerankerEndpoint
		if endpoint == "" {
			endpoint = "https://api.voyageai.com"
		}
		return rerank.Voyage{Endpoint: endpoint, Model: flags.rerankerModel, APIKey: providerAPIKey("DOCUWARDEN_RERANKER_API_KEY"), Client: client}, nil
	default:
		return nil, fmt.Errorf("unsupported reranker provider %q", flags.rerankerProvider)
	}
}

func providerAPIKey(name string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return os.Getenv("VOYAGE_API_KEY")
}

func (flags providerFlags) store() (*qdrantstore.Store, error) {
	return qdrantstore.New(qdrantstore.Config{Host: flags.qdrantHost, Port: flags.qdrantPort, APIKey: os.Getenv("DOCUWARDEN_QDRANT_API_KEY"), UseTLS: flags.qdrantTLS})
}

func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
func envInt(name string, fallback int) int {
	value, err := strconv.Atoi(os.Getenv(name))
	if err == nil {
		return value
	}
	return fallback
}
func envBool(name string, fallback bool) bool {
	value, err := strconv.ParseBool(os.Getenv(name))
	if err == nil {
		return value
	}
	return fallback
}
func safePath(value string) string {
	value = strings.TrimSpace(value)
	return strings.NewReplacer("/", "_", "\\", "_", "..", "_").Replace(value)
}
